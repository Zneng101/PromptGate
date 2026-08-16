// Package web 提供本地 Web UI 与管理 API：模板/版本 CRUD、缓存发布、渲染预览、审计日志。
package web

import (
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/zheng101/promptgate/internal/cache"
	"github.com/zheng101/promptgate/internal/engine"
	"github.com/zheng101/promptgate/internal/gateway"
	"github.com/zheng101/promptgate/internal/store"
)

// Server 聚合所有依赖，提供 HTTP 路由。
type Server struct {
	store   *store.Store
	cache   *cache.Cache
	engine  *engine.Engine
	gateway *gateway.Gateway
	mux     *http.ServeMux
}

// New 创建 Web Server 并注册路由。
func New(s *store.Store, c *cache.Cache, e *engine.Engine, g *gateway.Gateway) *Server {
	srv := &Server{store: s, cache: c, engine: e, gateway: g, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

// Handler 返回底层 mux，供上层 http.Server 使用。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	// 健康检查 & 运行时配置
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/config", s.getRuntimeConfig)
	s.mux.HandleFunc("GET /api/cache/status", s.cacheStatus)

	// 模板 CRUD
	s.mux.HandleFunc("GET /api/templates", s.listTemplates)
	s.mux.HandleFunc("POST /api/templates", s.createTemplate)
	s.mux.HandleFunc("GET /api/templates/{id}", s.getTemplate)
	s.mux.HandleFunc("PUT /api/templates/{id}", s.updateTemplate)
	s.mux.HandleFunc("DELETE /api/templates/{id}", s.deleteTemplate)

	// 版本 CRUD
	s.mux.HandleFunc("POST /api/templates/{id}/versions", s.addVersion)
	s.mux.HandleFunc("PUT /api/versions/{id}", s.updateVersion)
	s.mux.HandleFunc("DELETE /api/versions/{id}", s.deleteVersion)

	// 缓存发布（热更新）
	s.mux.HandleFunc("POST /api/publish", s.publish)

	// 渲染预览（Playground）
	s.mux.HandleFunc("POST /api/render", s.renderPreview)

	// 审计日志
	s.mux.HandleFunc("GET /api/audit", s.listAudit)

	// OpenAI 兼容代理由 gateway 处理
	s.mux.HandleFunc("/v1/chat/completions", s.gateway.ServeHTTP)

	// 静态资源（Web UI）
	s.mux.HandleFunc("/", s.serveStatic)
}

// ---------------- 基础接口 ----------------

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "cache_version": s.cache.Version()})
}

func (s *Server) getRuntimeConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"mock":     s.gateway.IsMock(),
		"base_url": "configured",
		"cache":    map[string]any{"version": s.cache.Version(), "templates": s.cache.Len()},
	})
}

func (s *Server) cacheStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": s.cache.Version(), "templates": s.cache.Len()})
}

// ---------------- 模板 CRUD ----------------

func (s *Server) listTemplates(w http.ResponseWriter, _ *http.Request) {
	ts, err := s.store.ListTemplates()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if ts == nil {
		ts = []store.PromptTemplate{}
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	var t store.PromptTemplate
	if err := decodeJSON(r, &t); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if t.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}
	if err := s.store.CreateTemplate(&t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	t, err := s.store.GetTemplate(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	var t store.PromptTemplate
	if err := decodeJSON(r, &t); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	t.ID = pathID(r)
	if err := s.store.UpdateTemplate(&t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTemplate(pathID(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------------- 版本 CRUD ----------------

func (s *Server) addVersion(w http.ResponseWriter, r *http.Request) {
	tmplID := pathID(r)
	var v store.Version
	if err := decodeJSON(r, &v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	v.TemplateID = tmplID
	if err := s.store.AddVersion(&v); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) updateVersion(w http.ResponseWriter, r *http.Request) {
	var v store.Version
	if err := decodeJSON(r, &v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	v.ID = pathID(r)
	// 保持 template_id 关联：若未提供，从现有记录读取
	if v.TemplateID == 0 {
		if existing, err := s.versionTemplateID(v.ID); err == nil {
			v.TemplateID = existing
		}
	}
	if err := s.store.UpdateVersion(&v); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) deleteVersion(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteVersion(pathID(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// versionTemplateID 通过遍历模板找到版本所属 template_id（简易实现）。
func (s *Server) versionTemplateID(versionID int64) (int64, error) {
	ts, err := s.store.ListTemplates()
	if err != nil {
		return 0, err
	}
	for _, t := range ts {
		for _, v := range t.Versions {
			if v.ID == versionID {
				return t.ID, nil
			}
		}
	}
	return 0, nil
}

// ---------------- 缓存发布 ----------------

// publish 重新从数据库加载全部模板，原子替换缓存（无需重启）。
func (s *Server) publish(w http.ResponseWriter, _ *http.Request) {
	ts, err := s.store.ListTemplates()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	data := make(map[string]*store.PromptTemplate, len(ts))
	for i := range ts {
		data[ts[i].Name] = &ts[i]
	}
	s.cache.AtomicUpdate(data)
	writeJSON(w, http.StatusOK, map[string]any{
		"published":     true,
		"cache_version": s.cache.Version(),
		"templates":     len(data),
	})
}

// ---------------- 渲染预览 ----------------

type renderRequest struct {
	Content   string                 `json:"content"`
	Variables map[string]interface{} `json:"variables"`
}

// renderPreview 在 Playground 中预览渲染结果，不调用 LLM。
func (s *Server) renderPreview(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "content is required"})
		return
	}
	out, err := s.engine.RenderPrompt(req.Content, req.Variables)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rendered": out})
}

// ---------------- 审计日志 ----------------

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := atoiSafe(l); err == nil {
			limit = int(n)
		}
	}
	logs, err := s.store.ListAuditLogs(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []store.AuditLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// ---------------- 静态资源 ----------------

// serveStatic 提供 Web UI 静态文件，并对未知路径回退到 index.html（SPA）。
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	// API/代理路径未命中时返回 404 JSON
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}

	clean := path.Clean(r.URL.Path)
	if clean == "/" || clean == "." {
		serveFileFS(w, r, "static/index.html")
		return
	}
	name := "static" + clean
	// 防止路径穿越
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	if !staticExists(name) {
		// SPA 回退
		serveFileFS(w, r, "static/index.html")
		return
	}
	serveFileFS(w, r, name)
}

// ---------------- helpers ----------------

func (s *Server) HandlerMux() *http.ServeMux { return s.mux }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}

func pathID(r *http.Request) int64 {
	id := r.PathValue("id")
	n, _ := atoiSafe(id)
	return n
}

func atoiSafe(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

var errInvalidInt = newParseErr("invalid integer")

type parseErr struct{ msg string }

func (e *parseErr) Error() string    { return e.msg }
func newParseErr(m string) *parseErr { return &parseErr{msg: m} }
