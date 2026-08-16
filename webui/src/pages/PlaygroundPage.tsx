import { useEffect, useState } from 'react'
import { api, streamChat } from '../api'
import type { PromptTemplate } from '../types'

export default function PlaygroundPage() {
  const [templates, setTemplates] = useState<PromptTemplate[]>([])
  const [templateName, setTemplateName] = useState('')
  const [versionIdx, setVersionIdx] = useState(0)
  const [content, setContent] = useState('')
  const [varsText, setVarsText] = useState('{}')
  const [rendered, setRendered] = useState('')
  const [renderErr, setRenderErr] = useState('')
  const [output, setOutput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [meta, setMeta] = useState<{ input_tokens: number; output_tokens: number } | null>(null)
  const [chatErr, setChatErr] = useState('')

  useEffect(() => {
    api.listTemplates().then((ts) => {
      setTemplates(ts)
      if (ts.length > 0) {
        setTemplateName(ts[0].name)
        setVersionIdx(0)
        setContent(ts[0].versions?.[0]?.content || '')
        setVarsText(ts[0].system_vars || '{}')
      }
    })
  }, [])

  const onTemplateChange = (name: string) => {
    const t = templates.find((x) => x.name === name)
    setTemplateName(name)
    setVersionIdx(0)
    if (t) {
      setContent(t.versions?.[0]?.content || '')
      setVarsText(t.system_vars || '{}')
    }
  }

  const onVersionChange = (idx: number) => {
    setVersionIdx(idx)
    const t = templates.find((x) => x.name === templateName)
    if (t?.versions?.[idx]) setContent(t.versions[idx].content)
  }

  const parseVars = (): Record<string, unknown> => {
    try {
      return JSON.parse(varsText || '{}')
    } catch {
      return {}
    }
  }

  const doRender = async () => {
    setRenderErr('')
    setRendered('')
    try {
      const r = await api.render(content, parseVars())
      setRendered(r.rendered)
    } catch (e) {
      setRenderErr(e instanceof Error ? e.message : String(e))
    }
  }

  const doChat = async () => {
    setStreaming(true)
    setOutput('')
    setChatErr('')
    setMeta(null)
    let acc = ''
    await streamChat(
      {
        template: templateName || undefined,
        variables: parseVars(),
        messages: [{ role: 'user', content: '请根据模板执行任务' }],
      },
      {
        onDelta: (t) => {
          acc += t
          setOutput(acc)
        },
        onMeta: (m) => setMeta(m),
        onDone: () => setStreaming(false),
        onError: (e) => {
          setChatErr(e)
          setStreaming(false)
        },
      },
    )
  }

  const currentTemplate = templates.find((t) => t.name === templateName)

  return (
    <div className="mx-auto flex h-full max-w-7xl gap-4 p-5">
      {/* 左：配置与模板 */}
      <div className="flex w-1/2 flex-col gap-4 overflow-auto pr-2">
        <h1 className="text-lg font-semibold text-gray-100">Playground</h1>

        <div className="rounded-xl border border-gray-800 bg-[#0d121b] p-4">
          <div className="mb-3 grid grid-cols-2 gap-3">
            <label className="block">
              <div className="mb-1 text-xs text-gray-500">选择模板</div>
              <select
                value={templateName}
                onChange={(e) => onTemplateChange(e.target.value)}
                className="input"
              >
                {templates.map((t) => (
                  <option key={t.id} value={t.name}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="block">
              <div className="mb-1 text-xs text-gray-500">版本（仅用于编辑预览）</div>
              <select
                value={versionIdx}
                onChange={(e) => onVersionChange(Number(e.target.value))}
                className="input"
              >
                {currentTemplate?.versions?.map((v, i) => (
                  <option key={v.id} value={i}>
                    {v.label} ({v.weight}%)
                  </option>
                ))}
              </select>
            </label>
          </div>

          <label className="mb-2 block">
            <div className="mb-1 text-xs text-gray-500">变量（JSON，灰度路由按 template 自动选择版本）</div>
            <textarea
              value={varsText}
              onChange={(e) => setVarsText(e.target.value)}
              className="input h-20 w-full resize-y font-mono text-xs"
            />
          </label>

          <label className="block">
            <div className="mb-1 flex items-center justify-between">
              <span className="text-xs text-gray-500">提示词正文（可编辑后预览渲染）</span>
              <button onClick={doRender} className="btn-ghost-sm">
                预览渲染
              </button>
            </div>
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              className="input h-48 w-full resize-y font-mono text-xs leading-relaxed"
            />
          </label>
        </div>

        <div className="rounded-xl border border-gray-800 bg-[#0d121b] p-4">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs font-medium text-gray-400">渲染结果</span>
            {renderErr && <span className="text-[11px] text-red-400">{renderErr}</span>}
          </div>
          <pre className="max-h-60 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-900/60 p-3 text-gray-300">
            {rendered || '点击「预览渲染」查看填充变量后的提示词'}
          </pre>
        </div>
      </div>

      {/* 右：对话输出 */}
      <div className="flex w-1/2 flex-col rounded-xl border border-gray-800 bg-[#0d121b] p-4">
        <div className="mb-3 flex items-center justify-between">
          <span className="text-xs font-medium text-gray-400">对话输出（流式）</span>
          <div className="flex items-center gap-2">
            {meta && (
              <span className="rounded bg-gray-800 px-2 py-0.5 text-[11px] text-gray-400">
                入 {meta.input_tokens} · 出 {meta.output_tokens} tok
              </span>
            )}
            <button
              onClick={doChat}
              disabled={streaming || !templateName}
              className="btn-primary-sm"
            >
              {streaming ? '生成中…' : '▶ 测试调用'}
            </button>
          </div>
        </div>
        <div className="flex-1 overflow-auto rounded-lg bg-gray-900/40 p-4">
          {output ? (
            <pre className="whitespace-pre-wrap text-sm leading-relaxed text-gray-200">{output}</pre>
          ) : (
            <div className="text-sm text-gray-600">
              {chatErr ? (
                <span className="text-red-400">{chatErr}</span>
              ) : (
                '点击「测试调用」通过 /v1/chat/completions 发起请求。未配置 API Key 时返回 Mock 预览。'
              )}
            </div>
          )}
          {streaming && <span className="ml-0.5 inline-block h-4 w-2 animate-pulse bg-violet-500 align-middle" />}
        </div>
      </div>
    </div>
  )
}
