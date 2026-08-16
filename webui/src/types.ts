// 前端类型定义，与 Go 端 store/models.go 对应。

export interface Version {
  id: number
  template_id: number
  label: string
  content: string
  model: string
  weight: number
  active: boolean
  created_at: string
}

// 创建版本时的输入（服务端自动生成 id/template_id/created_at）
export interface VersionInput {
  label: string
  content: string
  model?: string
  weight?: number
  active?: boolean
}

// 创建模板时的输入
export interface TemplateCreateInput {
  name: string
  description?: string
  system_vars?: string
  versions?: VersionInput[]
}

export interface PromptTemplate {
  id: number
  name: string
  description: string
  system_vars: string
  versions: Version[]
  created_at: string
  updated_at: string
}

export interface AuditLog {
  id: number
  trace_id: string
  user_id: string
  template_name: string
  version_label: string
  model: string
  input_tokens: number
  output_tokens: number
  status: string
  error: string
  created_at: string
}

export interface RuntimeConfig {
  mock: boolean
  base_url: string
  cache: { version: number; templates: number }
}

export interface RenderResult {
  rendered: string
  error?: string
}
