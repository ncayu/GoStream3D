import { useEffect, useState } from 'react'
import { PureStreamPlayer } from './components/PureStreamPlayer'
import { LocalCanvasPlayer } from './components/LocalCanvasPlayer'
import { AdminDashboard } from './components/AdminDashboard'
import { fetchWithTimeout } from './request'

function Viewer({ token }: { token: string }) {
  const [transport, setTransport] = useState<string | null>(null)
  const [renderPaused, setRenderPaused] = useState(false)
  const [showFPS, setShowFPS] = useState(true)
  useEffect(() => {
    const abort = new AbortController()
    fetchWithTimeout(`/api/v1/sessions/${encodeURIComponent(token)}`, { signal: abort.signal, cache: 'no-store' })
      .then(async (response) => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        return response.json() as Promise<{ data: { transport?: string; render_paused?: boolean; show_fps?: boolean } }>
      })
      .then((payload) => {
        setRenderPaused(Boolean(payload.data.render_paused))
        setShowFPS(payload.data.show_fps !== false)
        setTransport(payload.data.transport || 'webrtc')
      })
      .catch((error) => { if (!abort.signal.aborted) setTransport(`error: ${String(error)}`) })
    return () => abort.abort()
  }, [token])
  if (transport === null) return <main style={{ color: '#fff', background: '#000', minHeight: '100vh' }}>正在连接画面…</main>
  if (transport.startsWith('error:')) return <main style={{ color: '#fff', background: '#000', minHeight: '100vh' }}>{transport}</main>
  if (renderPaused) return <main style={{ color: '#fff', background: '#000', minHeight: '100vh', display: 'grid', placeItems: 'center' }}>渲染已由管理员停止，请稍后刷新页面。</main>
  if (transport === 'jpeg') return <LocalCanvasPlayer token={token} showFPS={showFPS} />
  return <PureStreamPlayer token={token} showFPS={showFPS} />
}

export default function App() {
  const match = /^\/(?:Stream3D|v)\/([A-Za-z0-9_-]+)\/?$/.exec(window.location.pathname)
  if (match) return <Viewer token={match[1]} />
  if (window.location.pathname === '/admin' || window.location.pathname === '/admin/') return <AdminDashboard />
  return <AdminDashboard />
}
