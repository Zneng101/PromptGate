package store

// PromptTemplate 是一个命名的提示词模板，包含多个灰度版本。
type PromptTemplate struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`         // 唯一标识，例如 "code-review"
	Description string    `json:"description"`  // 人类可读说明
	SystemVars  string    `json:"system_vars"`  // 默认变量 JSON，例如 {"lang":"go"}
	Versions    []Version `json:"versions"`     // 灰度版本列表
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
}

// Version 是某个模板的一个灰度版本。
type Version struct {
	ID         int64  `json:"id"`
	TemplateID int64  `json:"template_id"`
	Label      string `json:"label"`  // 版本标签，例如 "v1"、"v2-experiment"
	Content    string `json:"content"` // 提示词正文，支持 {{.var}} 占位
	Model      string `json:"model"`   // 目标模型，例如 gpt-4o
	Weight     int    `json:"weight"`  // 灰度百分比 0-100
	Active     bool   `json:"active"`  // 是否启用
	CreatedAt  string `json:"created_at"`
}

// AuditLog 记录一次代理转发的审计信息。
type AuditLog struct {
	ID            int64  `json:"id"`
	TraceID       string `json:"trace_id"`
	UserID        string `json:"user_id"`
	TemplateName  string `json:"template_name"`
	VersionLabel  string `json:"version_label"`
	Model         string `json:"model"`
	InputTokens   int    `json:"input_tokens"`
	OutputTokens  int    `json:"output_tokens"`
	Status        string `json:"status"` // ok / error
	Error         string `json:"error"`
	CreatedAt     string `json:"created_at"`
}
