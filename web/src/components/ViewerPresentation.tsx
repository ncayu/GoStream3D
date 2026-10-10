import { useEffect, useState } from 'react'
import { fetchWithTimeout } from '../request'

export function useViewerPresentation(token: string, initialShowFPS: boolean) {
  const [showFPS, setShowFPS] = useState(initialShowFPS)
  useEffect(() => {
    const controller = new AbortController()
    let inFlight = false
    setShowFPS(initialShowFPS)
    const sync = async () => {
      if (inFlight || document.visibilityState === 'hidden' || controller.signal.aborted) return
      inFlight = true
      try {
        const response = await fetchWithTimeout(`/api/v1/sessions/${encodeURIComponent(token)}`, { signal: controller.signal, cache: 'no-store' }, 5000)
        if (!response.ok) return
        const payload = await response.json() as { data: { show_fps?: boolean } }
        if (!controller.signal.aborted) setShowFPS(payload.data.show_fps !== false)
      } catch { /* Keep the last confirmed setting when the service is unavailable. */ }
      finally { inFlight = false }
    }
    const refresh = () => { void sync() }
    const timer = window.setInterval(refresh, 5000)
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      controller.abort()
      window.clearInterval(timer)
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [token, initialShowFPS])
  return { showFPS }
}

export function ViewerFPS({ value, visible }: { value: number; visible: boolean }) {
  if (!visible) return null
  return <div className="viewer-fps" aria-label="帧率显示">{value.toFixed(1)} FPS</div>
}
