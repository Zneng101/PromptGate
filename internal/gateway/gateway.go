// Package gateway 实现 OpenAI 兼容的代理转发核心：灰度路由 + 模板渲染 + 流式转发。
//
// 流程：
//
//	用户请求 → 提取 trace_id → 查本地缓存(原子读)
//	    → 命中模板 → 渲染(防注入)
//	    → 灰度路由(一致性 Hash) → 转发至真实 LLM
//	    → 接收流式响应 → 实时 Token 估算 → 返回客户端
//	    → 异步写 SQLite 审计日志(批量落盘)
package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zheng101/promptgate/internal/cache"
	"github.com/zheng101/promptgate/internal/engine"
	"github.com/zheng101/promptgate/internal/store"
	"github.com/zheng101/promptgate/pkg/tokenizer"
)

// UpstreamConfig 上游 LLM 配置。
type UpstreamConfig struct {
	APIKey  string // OpenAI API Key，为空时进入 Mock 模式
	BaseURL string // 例如 https://api.openai.com/v1
	Timeout time.Duration
}

// Gateway 代理网关。
type Gateway struct {
	cfg    UpstreamConfig
	cache  *cache.Cache
	store  *store.Store
	engine *engine.Engine
	client *http.Client
}

// New 创建网关。
func New(cfg UpstreamConfig, c *cache.Cache, s *store.Store, e *engine.Engine) *Gateway {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &Gateway{
		cfg:    cfg,
		cache:  c,
		store:  s,
		engine: e,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

// IsMock 返回是否处于 Mock 模式（未配置 API Key）。
func (g *Gateway) IsMock() bool { return g.cfg.APIKey == "" }

// SelectVersion 灰度路由：加权一致性哈希（支持百分比灰度 10% -> 90%）。
//
// 算法：根据 key（trace_id/user_id）计算 FNV-1a 32 位哈希，
// 按版本权重阈值命中。同一 key 永远命中同一版本（一致性）。
//
// 示例：key="user_123" 算出 67，灰度 10% 不中，20% 不中，90% 命中。
func SelectVersion(versions []store.Version, key string) *store.Version {
	active := make([]store.Version, 0, len(versions))
	for _, v := range versions {
		if v.Active {
			active = append(active, v)
		}
	}
	if len(active) == 0 {
		return nil
	}
	if len(active) == 1 {
		return &active[0]
	}

	// 计算哈希值（FNV-1a 32位）
	h := fnv.New32a()
	h.Write([]byte(key))
	hashVal := h.Sum32()

	// 按权重阈值命中
	totalWeight := 0
	for _, v := range active {
		totalWeight += v.Weight
	}
	if totalWeight <= 0 {
		// 权重全 0，等概率
		return &active[hashVal%uint32(len(active))]
	}
	threshold := hashVal % uint32(totalWeight)
	cum := 0
	for i := range active {
		cum += active[i].Weight
		if threshold < uint32(cum) {
			return &active[i]
		}
	}
	return &active[len(active)-1]
}

// chatRequest 是 OpenAI 兼容请求，额外支持 PromptGate 扩展字段。
type chatRequest struct {
	Model    string                   `json:"model,omitempty"`
	Messages []map[string]interface{} `json:"messages,omitempty"`
	Stream   bool                     `json:"stream,omitempty"`
	// PromptGate 扩展
	Template  string                 `json:"template,omitempty"`
	Variables map[string]interface{} `json:"variables,omitempty"`
	UserID    string                 `json:"user_id,omitempty"`
	TraceID   string                 `json:"trace_id,omitempty"`
}

// ServeHTTP 处理 /v1/chat/completions 请求（OpenAI 兼容）。
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	defer r.Body.Close()

	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	// 提取路由 key：优先 trace_id，其次 user_id，兜底随机
	traceID := firstNonEmpty(req.TraceID, r.Header.Get("X-Trace-Id"))
	userID := firstNonEmpty(req.UserID, r.Header.Get("X-User-Id"))
	routeKey := firstNonEmpty(traceID, userID)
	if routeKey == "" {
		routeKey = fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}

	var (
		tmpl       *store.PromptTemplate
		version    *store.Version
		rendered   string
		inputTokens int
		tmplName   string
		versionLabel string
		model      = req.Model
	)

	// 命中模板：查本地缓存（原子读）
	if req.Template != "" {
		var ok bool
		tmpl, ok = g.cache.Get(req.Template)
		if !ok {
			// 缓存未命中，回源数据库
			t, err := g.store.GetTemplateByName(req.Template)
			if err != nil || t == nil {
				writeError(w, http.StatusNotFound, "template not found: "+req.Template)
				return
			}
			tmpl = t
		}
		// 灰度路由
		version = SelectVersion(tmpl.Versions, routeKey)
		if version == nil {
			writeError(w, http.StatusServiceUnavailable, "no active version")
			return
		}

		// 合并变量：模板默认 system_vars + 请求 variables
		merged := mergeVars(tmpl.SystemVars, req.Variables)
		rendered, err = g.engine.RenderPrompt(version.Content, merged)
		if err != nil {
			writeError(w, http.StatusBadRequest, "render error: "+err.Error())
			return
		}
		inputTokens = tokenizer.EstimateTokens(rendered)
		tmplName = tmpl.Name
		versionLabel = version.Label
		if version.Model != "" {
			model = version.Model
		}
		if model == "" {
			model = "gpt-4o-mini"
		}

		// 将渲染结果注入为 system 消息（置于最前）
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": rendered,
		}
		req.Messages = append([]map[string]interface{}{systemMsg}, req.Messages...)
	}

	// Mock 模式：无需真实 Key 即可在 Playground 测试渲染效果
	if g.IsMock() {
		g.handleMock(w, r, req, rendered, tmplName, versionLabel, model, traceID, userID, inputTokens)
		return
	}

	// 转发至真实 LLM
	g.forwardUpstream(w, r, req, model, tmplName, versionLabel, traceID, userID, inputTokens)
}

// forwardUpstream 转发请求到上游 OpenAI 兼容接口，并流式回传 + 实时 Token 估算。
func (g *Gateway) forwardUpstream(w http.ResponseWriter, r *http.Request, req chatRequest, model, tmplName, versionLabel, traceID, userID string, inputTokens int) {
	// 重建转发体：剥离 PromptGate 扩展字段
	upReq := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
		"stream":   req.Stream,
	}
	payload, err := json.Marshal(upReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "marshal upstream: "+err.Error())
		return
	}

	url := strings.TrimRight(g.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "new upstream req: "+err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)
	// 透传客户端期望的流式标识
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := g.client.Do(httpReq)
	if err != nil {
		g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, 0, "error", err.Error())
		writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// 非 2xx 直接透传错误体
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, 0, "error", string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}

	// 流式：逐行转发并累计 token
	if req.Stream {
		g.streamSSE(w, resp.Body, traceID, userID, tmplName, versionLabel, model, inputTokens)
		return
	}

	// 非流式：读取完整响应，估算 token 后透传
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, 0, "error", err.Error())
		writeError(w, http.StatusBadGateway, "read upstream: "+err.Error())
		return
	}
	outputTokens := extractOutputTokens(respBody, req.Messages)
	g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, outputTokens, "ok", "")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respBody)
}

// streamSSE 逐行转发 SSE，累计 delta 内容并实时估算 Token。
func (g *Gateway) streamSSE(w http.ResponseWriter, body io.Reader, traceID, userID, tmplName, versionLabel, model string, inputTokens int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var deltas []string
	status := "ok"
	errMsg := ""

	for scanner.Scan() {
		line := scanner.Text()
		// 透传每一行
		_, _ = fmt.Fprintf(w, "%s\n", line)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}
			// 解析 delta.content 用于 token 估算
			if content := extractDeltaContent(data); content != "" {
				deltas = append(deltas, content)
			}
			// 解析最终 usage（部分上游在末尾返回）
			if u := extractUsageTokens(data); u > 0 {
				// 用上游真实 usage 覆盖估算值
				inputTokens, _ = extractUsageInput(data, inputTokens)
			}
		}
		if strings.TrimSpace(line) == "" {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		status = "error"
		errMsg = err.Error()
	}
	outputTokens := tokenizer.EstimateTokensFromDeltas(deltas)
	g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, outputTokens, status, errMsg)
	// 发送一条带 token 估算的注释事件给前端（不影响 OpenAI 客户端解析）
	_, _ = fmt.Fprintf(w, "event: promptgate\ndata: {\"input_tokens\":%d,\"output_tokens\":%d}\n\n", inputTokens, outputTokens)
	flusher.Flush()
}

// handleMock 在未配置 API Key 时返回模拟流式响应，便于 Playground 测试渲染。
func (g *Gateway) handleMock(w http.ResponseWriter, r *http.Request, req chatRequest, rendered, tmplName, versionLabel, model, traceID, userID string, inputTokens int) {
	preview := rendered
	if preview == "" {
		preview = joinMessages(req.Messages)
	}
	mockReply := fmt.Sprintf("[PromptGate Mock 模式] 未配置 API Key，以下为渲染后的提示词预览：\n\n%s", truncate(preview, 800))
	outputTokens := tokenizer.EstimateTokens(mockReply)

	if !req.Stream {
		resp := map[string]interface{}{
			"id":      "mock-" + traceID,
			"object":  "chat.completion",
			"model":   model,
			"choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": mockReply}, "finish_reason": "stop"}},
			"usage":   map[string]interface{}{"prompt_tokens": inputTokens, "completion_tokens": outputTokens, "total_tokens": inputTokens + outputTokens},
		}
		g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, outputTokens, "mock", "")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := "mock-" + traceID
	// 按字符切片模拟流式
	runes := []rune(mockReply)
	for i := 0; i < len(runes); i += 4 {
		end := i + 4
		if end > len(runes) {
			end = len(runes)
		}
		chunk := map[string]interface{}{
			"id":      id,
			"object":  "chat.completion.chunk",
			"model":   model,
			"choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"content": string(runes[i:end])}}},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	_, _ = fmt.Fprintf(w, "event: promptgate\ndata: {\"input_tokens\":%d,\"output_tokens\":%d}\n\n", inputTokens, outputTokens)
	flusher.Flush()
	g.audit(traceID, userID, tmplName, versionLabel, model, inputTokens, outputTokens, "mock", "")
}

// audit 异步写入审计日志（批量落盘由 SQLite WAL 承载）。
func (g *Gateway) audit(traceID, userID, tmplName, versionLabel, model string, inputTokens, outputTokens int, status, errMsg string) {
	go func() {
		l := &store.AuditLog{
			TraceID:      traceID,
			UserID:       userID,
			TemplateName: tmplName,
			VersionLabel: versionLabel,
			Model:        model,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Status:       status,
			Error:        truncate(errMsg, 500),
		}
		if err := g.store.InsertAuditLog(l); err != nil {
			log.Printf("[audit] write failed: %v", err)
		}
	}()
}

// ---------------- 辅助函数 ----------------

func (g *Gateway) WarmFromStore(ctx context.Context) error {
	templates, err := g.store.ListTemplates()
	if err != nil {
		return err
	}
	data := make(map[string]*store.PromptTemplate, len(templates))
	for i := range templates {
		data[templates[i].Name] = &templates[i]
	}
	g.cache.AtomicUpdate(data)
	log.Printf("[cache] warmed %d templates, version=%d", len(data), g.cache.Version())
	return nil
}

// mergeVars 合并模板默认变量与请求变量（请求覆盖默认）。
func mergeVars(systemVarsJSON string, reqVars map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	if systemVarsJSON != "" && systemVarsJSON != "{}" {
		_ = json.Unmarshal([]byte(systemVarsJSON), &out)
	}
	for k, v := range reqVars {
		out[k] = v
	}
	return out
}

// extractDeltaContent 从单条 SSE data 中提取 delta.content。
func extractDeltaContent(data string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return ""
	}
	choices, ok := m["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	first, ok := choices[0].(map[string]interface{})
	if !ok {
		return ""
	}
	delta, ok := first["delta"].(map[string]interface{})
	if !ok {
		return ""
	}
	if c, ok := delta["content"].(string); ok {
		return c
	}
	return ""
}

// extractUsageTokens 从 SSE data 中提取 usage.completion_tokens（若上游返回）。
func extractUsageTokens(data string) int {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return 0
	}
	if u, ok := m["usage"].(map[string]interface{}); ok {
		if c, ok := u["completion_tokens"].(float64); ok {
			return int(c)
		}
	}
	return 0
}

// extractUsageInput 从 usage 中提取 prompt_tokens，失败则返回原值。
func extractUsageInput(data string, fallback int) (int, int) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return fallback, fallback
	}
	if u, ok := m["usage"].(map[string]interface{}); ok {
		if p, ok := u["prompt_tokens"].(float64); ok {
			return int(p), int(p)
		}
	}
	return fallback, fallback
}

// extractOutputTokens 从非流式完整响应中提取 token 数，失败则估算。
func extractOutputTokens(body []byte, messages []map[string]interface{}) int {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err == nil {
		if u, ok := m["usage"].(map[string]interface{}); ok {
			if c, ok := u["completion_tokens"].(float64); ok && c > 0 {
				return int(c)
			}
		}
	}
	// 估算：拼接所有 assistant 消息
	var sb strings.Builder
	for _, msg := range messages {
		if role, _ := msg["role"].(string); role == "assistant" {
			if c, ok := msg["content"].(string); ok {
				sb.WriteString(c)
			}
		}
	}
	return tokenizer.EstimateTokens(sb.String())
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{"message": msg, "type": "promptgate_error"},
	})
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func joinMessages(msgs []map[string]interface{}) string {
	var sb strings.Builder
	for _, m := range msgs {
		if c, ok := m["content"].(string); ok {
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
