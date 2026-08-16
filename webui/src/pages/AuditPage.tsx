import { useEffect, useState } from 'react'
import { api } from '../api'
import type { AuditLog } from '../types'

export default function AuditPage() {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setLogs(await api.listAudit(200))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const statusColor = (s: string) =>
    s === 'ok'
      ? 'text-emerald-400'
      : s === 'mock'
        ? 'text-amber-400'
        : 'text-red-400'

  return (
    <div className="mx-auto max-w-7xl px-6 py-5">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-gray-100">审计日志</h1>
        <button onClick={load} className="btn-ghost-sm">
          ↻ 刷新
        </button>
      </div>

      {error && <div className="mb-3 text-sm text-red-400">{error}</div>}

      <div className="overflow-hidden rounded-xl border border-gray-800">
        <table className="w-full text-sm">
          <thead className="bg-gray-900/80 text-xs text-gray-500">
            <tr>
              <th className="px-3 py-2.5 text-left font-medium">时间</th>
              <th className="px-3 py-2.5 text-left font-medium">模板</th>
              <th className="px-3 py-2.5 text-left font-medium">版本</th>
              <th className="px-3 py-2.5 text-left font-medium">模型</th>
              <th className="px-3 py-2.5 text-left font-medium">Trace</th>
              <th className="px-3 py-2.5 text-right font-medium">入 Tokens</th>
              <th className="px-3 py-2.5 text-right font-medium">出 Tokens</th>
              <th className="px-3 py-2.5 text-left font-medium">状态</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800/80">
            {loading && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-gray-500">
                  加载中…
                </td>
              </tr>
            )}
            {!loading && logs.length === 0 && (
              <tr>
                <td colSpan={8} className="px-3 py-6 text-center text-gray-500">
                  暂无记录，前往 Playground 发起一次测试调用
                </td>
              </tr>
            )}
            {logs.map((l) => (
              <tr key={l.id} className="text-gray-300 hover:bg-gray-900/40">
                <td className="whitespace-nowrap px-3 py-2 text-xs text-gray-400">
                  {new Date(l.created_at).toLocaleString('zh-CN', { hour12: false })}
                </td>
                <td className="px-3 py-2">{l.template_name || '-'}</td>
                <td className="px-3 py-2 text-xs">{l.version_label || '-'}</td>
                <td className="px-3 py-2 font-mono text-xs text-gray-400">{l.model || '-'}</td>
                <td className="px-3 py-2 font-mono text-xs text-gray-500">
                  {l.trace_id ? l.trace_id.slice(0, 12) : '-'}
                </td>
                <td className="px-3 py-2 text-right tabular-nums">{l.input_tokens}</td>
                <td className="px-3 py-2 text-right tabular-nums">{l.output_tokens}</td>
                <td className={`px-3 py-2 text-xs font-medium ${statusColor(l.status)}`}>
                  {l.status}
                  {l.error && <div className="mt-0.5 max-w-xs truncate text-[11px] text-red-500" title={l.error}>{l.error}</div>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
