import { useEffect, useState } from 'react'
import type { NavigateFunction } from 'react-router-dom'
import { api } from '../api'
import type { PromptTemplate, Version } from '../types'

export default function TemplatesPage({ onNavigate }: { onNavigate: NavigateFunction }) {
  const [templates, setTemplates] = useState<PromptTemplate[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const ts = await api.listTemplates()
      setTemplates(ts)
      if (selectedId == null && ts.length > 0) setSelectedId(ts[0].id)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const selected = templates.find((t) => t.id === selectedId) || null

  const createTemplate = async () => {
    try {
      const t = await api.createTemplate({
        name: `new-template-${Date.now() % 10000}`,
        description: '新建模板',
        system_vars: '{}',
        versions: [
          { label: 'v1', content: '你是一位助手。任务：{{.task}}\n输入：{{.input}}', model: 'gpt-4o-mini', weight: 100, active: true },
        ],
      })
      await load()
      setSelectedId(t.id)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="flex h-full">
      {/* 模板列表 */}
      <div className="flex w-64 shrink-0 flex-col border-r border-gray-800 bg-[#0d121b]">
        <div className="flex items-center justify-between px-4 py-3">
          <h2 className="text-sm font-semibold text-gray-200">模板列表</h2>
          <button
            onClick={createTemplate}
            className="rounded bg-gray-800 px-2 py-1 text-xs text-gray-200 hover:bg-gray-700"
          >
            + 新建
          </button>
        </div>
        <div className="flex-1 overflow-auto px-2 pb-3">
          {loading && <div className="px-3 py-4 text-xs text-gray-500">加载中…</div>}
          {error && <div className="px-3 py-2 text-xs text-red-400">{error}</div>}
          {templates.map((t) => (
            <button
              key={t.id}
              onClick={() => setSelectedId(t.id)}
              className={`mb-1 w-full rounded-lg px-3 py-2 text-left transition ${
                t.id === selectedId
                  ? 'bg-violet-600/15 text-violet-300'
                  : 'text-gray-300 hover:bg-gray-800/60'
              }`}
            >
              <div className="truncate text-sm font-medium">{t.name}</div>
              <div className="mt-0.5 flex items-center gap-2 text-[11px] text-gray-500">
                <span>{t.versions?.length || 0} 版本</span>
                <span>·</span>
                <span className="truncate">{t.description}</span>
              </div>
            </button>
          ))}
          {!loading && templates.length === 0 && (
            <div className="px-3 py-4 text-xs text-gray-500">暂无模板，点击「新建」创建</div>
          )}
        </div>
      </div>

      {/* 编辑区 */}
      <div className="flex-1 overflow-auto">
        {selected ? (
          <TemplateEditor template={selected} onChanged={load} onNavigate={onNavigate} />
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-gray-500">
            选择左侧模板或新建一个模板开始
          </div>
        )}
      </div>
    </div>
  )
}

function TemplateEditor({
  template,
  onChanged,
  onNavigate,
}: {
  template: PromptTemplate
  onChanged: () => void
  onNavigate: NavigateFunction
}) {
  const [name, setName] = useState(template.name)
  const [description, setDescription] = useState(template.description)
  const [systemVars, setSystemVars] = useState(template.system_vars)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    setName(template.name)
    setDescription(template.description)
    setSystemVars(template.system_vars)
  }, [template.id])

  const save = async () => {
    setSaving(true)
    try {
      await api.updateTemplate(template.id, { name, description, system_vars: systemVars })
      setMsg('已保存')
      onChanged()
      setTimeout(() => setMsg(''), 2000)
    } catch (e) {
      setMsg(`保存失败：${e instanceof Error ? e.message : e}`)
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!confirm(`确认删除模板「${template.name}」及其所有版本？`)) return
    await api.deleteTemplate(template.id)
    onChanged()
  }

  return (
    <div className="mx-auto max-w-4xl px-6 py-5">
      <div className="mb-5 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-gray-100">编辑模板</h1>
        <div className="flex items-center gap-2">
          {msg && <span className="text-xs text-gray-400">{msg}</span>}
          <button
            onClick={() => onNavigate('/playground')}
            className="rounded-lg border border-gray-700 px-3 py-1.5 text-xs text-gray-300 hover:bg-gray-800"
          >
            在 Playground 测试 →
          </button>
          <button
            onClick={save}
            disabled={saving}
            className="rounded-lg bg-violet-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-violet-500 disabled:opacity-50"
          >
            {saving ? '保存中…' : '保存'}
          </button>
          <button onClick={remove} className="rounded-lg border border-red-800 px-3 py-1.5 text-xs text-red-400 hover:bg-red-900/30">
            删除
          </button>
        </div>
      </div>

      <div className="space-y-4 rounded-xl border border-gray-800 bg-[#0d121b] p-5">
        <Field label="模板名称（唯一标识，调用时使用）">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="input"
            placeholder="例如 code-review"
          />
        </Field>
        <Field label="描述">
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="input"
            placeholder="模板用途说明"
          />
        </Field>
        <Field label="默认变量（JSON）">
          <input
            value={systemVars}
            onChange={(e) => setSystemVars(e.target.value)}
            className="input font-mono text-xs"
            placeholder='{"lang":"go"}'
          />
        </Field>
      </div>

      <div className="mt-6">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-gray-200">
            灰度版本 <span className="text-gray-500">（{template.versions?.length || 0}）</span>
          </h2>
          <span className="text-[11px] text-gray-500">权重之和即总流量，按百分比灰度</span>
        </div>
        <div className="space-y-4">
          {template.versions?.map((v) => (
            <VersionCard key={v.id} version={v} onChanged={onChanged} />
          ))}
        </div>
        <AddVersion templateId={template.id} onAdded={onChanged} />
      </div>
    </div>
  )
}

function VersionCard({ version, onChanged }: { version: Version; onChanged: () => void }) {
  const [label, setLabel] = useState(version.label)
  const [content, setContent] = useState(version.content)
  const [model, setModel] = useState(version.model)
  const [weight, setWeight] = useState(version.weight)
  const [active, setActive] = useState(version.active)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    setLabel(version.label)
    setContent(version.content)
    setModel(version.model)
    setWeight(version.weight)
    setActive(version.active)
  }, [version.id])

  const save = async () => {
    setSaving(true)
    try {
      await api.updateVersion(version.id, { label, content, model, weight, active })
      setMsg('已保存')
      onChanged()
      setTimeout(() => setMsg(''), 2000)
    } catch (e) {
      setMsg(`失败：${e instanceof Error ? e.message : e}`)
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!confirm(`删除版本「${version.label}」？`)) return
    await api.deleteVersion(version.id)
    onChanged()
  }

  return (
    <div className="rounded-xl border border-gray-800 bg-[#0d121b] p-4">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <input
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          className="input w-40"
          placeholder="版本标签"
        />
        <input
          value={model}
          onChange={(e) => setModel(e.target.value)}
          className="input w-44 font-mono text-xs"
          placeholder="模型"
        />
        <div className="flex items-center gap-2">
          <span className="text-xs text-gray-500">权重</span>
          <input
            type="number"
            min={0}
            max={100}
            value={weight}
            onChange={(e) => setWeight(Number(e.target.value))}
            className="input w-20"
          />
          <span className="text-xs text-gray-500">%</span>
        </div>
        <label className="flex cursor-pointer items-center gap-1.5 text-xs text-gray-400">
          <input
            type="checkbox"
            checked={active}
            onChange={(e) => setActive(e.target.checked)}
            className="accent-violet-500"
          />
          启用
        </label>
        <div className="ml-auto flex items-center gap-2">
          {msg && <span className="text-[11px] text-gray-400">{msg}</span>}
          <button onClick={save} disabled={saving} className="btn-primary-sm">
            {saving ? '…' : '保存'}
          </button>
          <button onClick={remove} className="btn-danger-sm">删除</button>
        </div>
      </div>
      <textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        className="input h-40 w-full resize-y font-mono text-xs leading-relaxed"
        placeholder="提示词正文，支持 {{.var}} 占位符"
      />
      <div className="mt-1 text-[11px] text-gray-600">支持变量：{'{{.var}}'} · 缺失字段自动补零值 · 禁止 range/define/call</div>
    </div>
  )
}

function AddVersion({ templateId, onAdded }: { templateId: number; onAdded: () => void }) {
  const [open, setOpen] = useState(false)
  const [label, setLabel] = useState('')
  const add = async () => {
    if (!label.trim()) return
    await api.addVersion(templateId, {
      label,
      content: '请处理：{{.input}}',
      model: 'gpt-4o-mini',
      weight: 0,
      active: false,
    })
    setLabel('')
    setOpen(false)
    onAdded()
  }
  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="mt-4 w-full rounded-xl border border-dashed border-gray-700 py-3 text-xs text-gray-500 hover:border-gray-600 hover:text-gray-400"
      >
        + 添加版本
      </button>
    )
  }
  return (
    <div className="mt-4 flex items-center gap-2 rounded-xl border border-gray-800 bg-[#0d121b] p-3">
      <input
        value={label}
        onChange={(e) => setLabel(e.target.value)}
        className="input flex-1"
        placeholder="新版本标签，例如 v2-experiment"
        autoFocus
      />
      <button onClick={add} className="btn-primary-sm">添加</button>
      <button onClick={() => setOpen(false)} className="btn-ghost-sm">取消</button>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="mb-1 text-xs text-gray-500">{label}</div>
      {children}
    </label>
  )
}
