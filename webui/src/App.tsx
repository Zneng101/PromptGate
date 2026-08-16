import { NavLink, Route, Routes, useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { api } from './api'
import type { RuntimeConfig } from './types'
import TemplatesPage from './pages/TemplatesPage'
import PlaygroundPage from './pages/PlaygroundPage'
import AuditPage from './pages/AuditPage'

export default function App() {
  const [cfg, setCfg] = useState<RuntimeConfig | null>(null)
  const [publishing, setPublishing] = useState(false)
  const [publishMsg, setPublishMsg] = useState('')
  const navigate = useNavigate()

  const refreshCfg = () => api.getConfig().then(setCfg).catch(() => {})
  useEffect(() => {
    refreshCfg()
  }, [])

  const publish = async () => {
    setPublishing(true)
    setPublishMsg('')
    try {
      const r = await api.publish()
      setPublishMsg(`已发布 v${r.cache_version}（${r.templates} 个模板）`)
      refreshCfg()
    } catch (e) {
      setPublishMsg(`发布失败：${e instanceof Error ? e.message : e}`)
    } finally {
      setPublishing(false)
      setTimeout(() => setPublishMsg(''), 3000)
    }
  }

  return (
    <div className="flex h-full">
      {/* 侧边栏 */}
      <aside className="flex w-60 shrink-0 flex-col border-r border-gray-800 bg-[#0d121b]">
        <div className="flex items-center gap-2 px-5 py-5">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-gradient-to-br from-violet-500 to-indigo-600 text-lg font-bold text-white">
            P
          </div>
          <div>
            <div className="text-sm font-semibold text-gray-100">PromptGate</div>
            <div className="text-[11px] text-gray-500">提示词灰度网关</div>
          </div>
        </div>

        <nav className="mt-2 flex flex-col gap-1 px-3">
          <NavItem to="/templates" label="模板管理" icon="▦" />
          <NavItem to="/playground" label="Playground" icon="▶" />
          <NavItem to="/audit" label="审计日志" icon="≣" />
        </nav>

        <div className="mt-auto px-3 pb-4">
          <div className="mb-3 rounded-lg border border-gray-800 bg-gray-900/60 p-3 text-xs">
            <div className="mb-1 flex items-center justify-between">
              <span className="text-gray-500">运行模式</span>
              <span
                className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
                  cfg?.mock ? 'bg-amber-500/15 text-amber-400' : 'bg-emerald-500/15 text-emerald-400'
                }`}
              >
                {cfg?.mock ? 'Mock' : '代理'}
              </span>
            </div>
            <div className="flex items-center justify-between text-gray-500">
              <span>缓存版本</span>
              <span className="text-gray-300">v{cfg?.cache?.version ?? '-'}</span>
            </div>
            <div className="mt-1 flex items-center justify-between text-gray-500">
              <span>模板数</span>
              <span className="text-gray-300">{cfg?.cache?.templates ?? '-'}</span>
            </div>
          </div>

          <button
            onClick={publish}
            disabled={publishing}
            className="w-full rounded-lg bg-violet-600 px-3 py-2 text-sm font-medium text-white transition hover:bg-violet-500 disabled:opacity-50"
          >
            {publishing ? '发布中…' : '⚡ 发布到缓存'}
          </button>
          {publishMsg && (
            <div className="mt-2 text-center text-[11px] text-gray-400">{publishMsg}</div>
          )}
        </div>
      </aside>

      {/* 主内容 */}
      <main className="flex-1 overflow-auto">
        <Routes>
          <Route path="/" element={<TemplatesPage onNavigate={navigate} />} />
          <Route path="/templates" element={<TemplatesPage onNavigate={navigate} />} />
          <Route path="/playground" element={<PlaygroundPage />} />
          <Route path="/audit" element={<AuditPage />} />
        </Routes>
      </main>
    </div>
  )
}

function NavItem({ to, label, icon }: { to: string; label: string; icon: string }) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        `flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition ${
          isActive
            ? 'bg-violet-600/15 text-violet-300'
            : 'text-gray-400 hover:bg-gray-800/60 hover:text-gray-200'
        }`
      }
    >
      <span className="w-4 text-center text-xs opacity-70">{icon}</span>
      {label}
    </NavLink>
  )
}
