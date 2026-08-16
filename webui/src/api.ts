// 与后端 REST API 交互的客户端。
import type { AuditLog, PromptTemplate, RenderResult, RuntimeConfig, TemplateCreateInput, Version } from './types'

const BASE = ''

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!resp.ok) {
    let msg = `${resp.status} ${resp.statusText}`
    try {
      const body = await resp.json()
      if (body?.error?.message) msg = body.error.message
      else if (body?.error) msg = String(body.error)
    } catch {
      /* ignore */
    }
    throw new Error(msg)
  }
  if (resp.status === 204) return undefined as T
  return resp.json() as Promise<T>
}

export const api = {
  // 模板
  listTemplates: () => json<PromptTemplate[]>(`${BASE}/api/templates`),
  getTemplate: (id: number) => json<PromptTemplate>(`${BASE}/api/templates/${id}`),
  createTemplate: (t: TemplateCreateInput) =>
    json<PromptTemplate>(`${BASE}/api/templates`, { method: 'POST', body: JSON.stringify(t) }),
  updateTemplate: (id: number, t: Partial<PromptTemplate>) =>
    json<PromptTemplate>(`${BASE}/api/templates/${id}`, { method: 'PUT', body: JSON.stringify(t) }),
  deleteTemplate: (id: number) =>
    json<{ deleted: boolean }>(`${BASE}/api/templates/${id}`, { method: 'DELETE' }),

  // 版本
  addVersion: (templateId: number, v: Partial<Version>) =>
    json<Version>(`${BASE}/api/templates/${templateId}/versions`, { method: 'POST', body: JSON.stringify(v) }),
  updateVersion: (id: number, v: Partial<Version>) =>
    json<Version>(`${BASE}/api/versions/${id}`, { method: 'PUT', body: JSON.stringify(v) }),
  deleteVersion: (id: number) =>
    json<{ deleted: boolean }>(`${BASE}/api/versions/${id}`, { method: 'DELETE' }),

  // 缓存发布
  publish: () =>
    json<{ published: boolean; cache_version: number; templates: number }>(`${BASE}/api/publish`, {
      method: 'POST',
    }),

  // 渲染预览
  render: (content: string, variables: Record<string, unknown>) =>
    json<RenderResult>(`${BASE}/api/render`, {
      method: 'POST',
      body: JSON.stringify({ content, variables }),
    }),

  // 审计
  listAudit: (limit = 100) => json<AuditLog[]>(`${BASE}/api/audit?limit=${limit}`),

  // 运行时配置
  getConfig: () => json<RuntimeConfig>(`${BASE}/api/config`),
}

// 流式聊天：通过 OpenAI 兼容接口 /v1/chat/completions 转发，解析 SSE。
export interface StreamCallbacks {
  onDelta: (text: string) => void
  onMeta?: (meta: { input_tokens: number; output_tokens: number }) => void
  onDone: () => void
  onError: (err: string) => void
}

export async function streamChat(
  body: {
    template?: string
    variables?: Record<string, unknown>
    content?: string
    messages?: Array<{ role: string; content: string }>
    stream?: boolean
    user_id?: string
    trace_id?: string
  },
  cb: StreamCallbacks,
): Promise<void> {
  try {
    const resp = await fetch(`${BASE}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...body, stream: true }),
    })
    if (!resp.ok || !resp.body) {
      const text = await resp.text().catch(() => '')
      cb.onError(text || `${resp.status} ${resp.statusText}`)
      return
    }
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed) continue
        if (trimmed.startsWith('event: promptgate')) {
          // 下一条 data 为 token 元信息，标记后由后续 data 处理
          continue
        }
        if (trimmed.startsWith('data: ')) {
          const data = trimmed.slice(6)
          if (data === '[DONE]') {
            cb.onDone()
            return
          }
          try {
            const obj = JSON.parse(data)
            const delta = obj?.choices?.[0]?.delta?.content
            if (delta) cb.onDelta(delta)
            const usage = obj?.usage
            if (usage) cb.onMeta?.({ input_tokens: usage.prompt_tokens, output_tokens: usage.completion_tokens })
          } catch {
            // 可能是 promptgate meta 行
            try {
              const obj = JSON.parse(data)
              if (obj.input_tokens !== undefined) cb.onMeta?.(obj)
            } catch {
              /* ignore */
            }
          }
        }
      }
    }
    cb.onDone()
  } catch (e) {
    cb.onError(e instanceof Error ? e.message : String(e))
  }
}
