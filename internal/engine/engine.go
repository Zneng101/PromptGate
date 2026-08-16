// Package engine 实现提示词模板的安全渲染引擎。
//
// 核心是「白名单字段映射 + 严格零值默认」的防注入沙箱：
//   - 仅暴露 sanitizeMap 清理后的纯数据结构（map/slice/基本类型），从根源上
//     杜绝通过变量访问结构体方法或调用任意函数；
//   - 解析阶段拒绝 {{range}}/{{template}}/{{define}}/{{block}}/{{call}} 等危险动作；
//   - 使用 missingkey=zero 选项，缺失字段自动补零值而非报错。
package engine

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"
)

// forbiddenActions 是被禁止的模板动作关键字。
var forbiddenActions = map[string]bool{
	"range":    true, // 禁止循环，防止遍历注入
	"template": true, // 禁止模板嵌套调用
	"define":   true, // 禁止运行时定义模板
	"block":    true, // 禁止块定义
	"call":     true, // 禁止函数调用
}

// Engine 模板渲染引擎。复用无状态，可并发调用 Render。
type Engine struct{}

// New 创建引擎。
func New() *Engine { return &Engine{} }

// RenderPrompt 渲染模板字符串。
//
// 算法步骤：
//  1. 预检模板：拒绝危险动作（range/template/define/block/call）。
//  2. 构建 FuncMap：仅暴露 "safe" 透传函数。
//  3. 以 missingkey=zero 选项解析，缺失字段补零值。
//  4. 执行时传入 sanitizeMap 递归清理后的纯数据上下文，
//     锁定变量层级，禁止访问结构体方法。
func (e *Engine) RenderPrompt(tmplStr string, vars map[string]interface{}) (string, error) {
	if err := precheck(tmplStr); err != nil {
		return "", err
	}
	funcMap := template.FuncMap{
		"safe": func(s string) string { return s }, // 仅透传
	}
	tmpl, err := template.New("p").Funcs(funcMap).Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, sanitizeMap(vars)); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// precheck 解析模板并遍历语法树，拒绝危险动作。
func precheck(tmplStr string) error {
	treeSet, err := parse.Parse("p", tmplStr, "", "", template.FuncMap{"safe": func(s string) string { return s }})
	if err != nil {
		return fmt.Errorf("precheck parse: %w", err)
	}
	// {{define}}/{{block}} 会在 treeSet 中产生额外的树，多于一个即视为危险动作
	if len(treeSet) > 1 {
		return fmt.Errorf("禁止的动作: {{define}}/{{block}}")
	}
	for _, tree := range treeSet {
		if err := walkTree(tree); err != nil {
			return err
		}
	}
	return nil
}

func walkTree(t *parse.Tree) error {
	return walkNodes(t.Root)
}

func walkNodes(n parse.Node) error {
	switch x := n.(type) {
	case *parse.ListNode:
		if x == nil {
			return nil
		}
		for _, child := range x.Nodes {
			if err := walkNodes(child); err != nil {
				return err
			}
		}
	case *parse.RangeNode:
		return fmt.Errorf("禁止的动作: {{range}}")
	case *parse.TemplateNode:
		return fmt.Errorf("禁止的动作: {{template}}")
	case *parse.IfNode:
		if err := walkPipe(x.Pipe); err != nil {
			return err
		}
		if err := walkNodes(x.List); err != nil {
			return err
		}
		if x.ElseList != nil {
			if err := walkNodes(x.ElseList); err != nil {
				return err
			}
		}
	case *parse.WithNode:
		if err := walkPipe(x.Pipe); err != nil {
			return err
		}
		if err := walkNodes(x.List); err != nil {
			return err
		}
		if x.ElseList != nil {
			if err := walkNodes(x.ElseList); err != nil {
				return err
			}
		}
	case *parse.ActionNode:
		return walkPipe(x.Pipe)
	}
	return nil
}

// walkPipe 检查管道中的命令，拒绝 call 标识符。
func walkPipe(p *parse.PipeNode) error {
	if p == nil {
		return nil
	}
	for _, cmd := range p.Cmds {
		for _, arg := range cmd.Args {
			if id, ok := arg.(*parse.IdentifierNode); ok {
				if forbiddenActions[strings.ToLower(id.Ident)] {
					return fmt.Errorf("禁止的动作: {{%s}}", id.Ident)
				}
			}
		}
	}
	return nil
}

// ---------------- sanitizeMap 递归清理 ----------------

// sanitizeMap 将任意 map 递归转换为只包含安全基本类型的结构。
// 任何函数、结构体方法、不安全类型都被剥离，从根源上杜绝方法调用注入。
func sanitizeMap(vars map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(vars))
	for k, v := range vars {
		// 键名白名单：只允许字母、数字、下划线、点
		if !isSafeKey(k) {
			continue
		}
		out[k] = sanitizeValue(v)
	}
	return out
}

// sanitizeValue 递归归一化值，只保留 string/int/float64/bool/[]interface{}/map[string]interface{}。
func sanitizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return x
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return x
	case float32, float64:
		return x
	case map[string]interface{}:
		return sanitizeMap(x)
	case []interface{}:
		arr := make([]interface{}, len(x))
		for i, e := range x {
			arr[i] = sanitizeValue(e)
		}
		return arr
	case []map[string]interface{}:
		arr := make([]interface{}, len(x))
		for i, e := range x {
			arr[i] = sanitizeMap(e)
		}
		return arr
	default:
		// 未知类型（含 struct、func 等）一律降级为字符串，丢弃方法
		return fmt.Sprintf("%v", x)
	}
}

// isSafeKey 校验键名只包含字母、数字、下划线、点。
func isSafeKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if !(r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
