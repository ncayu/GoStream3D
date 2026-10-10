import { useCallback, useEffect, useRef, useState } from 'react'
import { defaultTheme, isThemeId, type ThemeId } from './themes'

type Request = (path: string, init?: RequestInit) => Promise<Response>
export type ThemeSaveResult = { success: boolean; message: string }

async function readTheme(response: Response): Promise<ThemeId> {
  const body = await response.json() as { data?: { theme?: unknown } }
  if (!isThemeId(body.data?.theme)) throw new Error('服务端返回的配色不合法，请重试。')
  return body.data.theme
}

export function useAppearance(authenticated: boolean, request: Request) {
  const [theme, setTheme] = useState<ThemeId>(defaultTheme)
  const [savedTheme, setSavedTheme] = useState<ThemeId | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [result, setResult] = useState<ThemeSaveResult | null>(null)
  const [revision, setRevision] = useState(0)
  const saveRequest = useRef<AbortController | null>(null)

  useEffect(() => {
    setSavedTheme(null)
    setError('')
    setResult(null)
    setTheme(defaultTheme)
    if (!authenticated) return
    const controller = new AbortController()
    setLoading(true)
    void request('/api/v1/admin/appearance', { signal: controller.signal })
      .then(readTheme)
      .then(value => { if (!controller.signal.aborted) { setTheme(value); setSavedTheme(value) } })
      .catch(cause => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '无法读取已保存的配色，请重试。') })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => { controller.abort(); saveRequest.current?.abort(); saveRequest.current = null; setSaving(false); setLoading(false) }
  }, [authenticated, request, revision])

  const save = async () => {
    if (!authenticated || savedTheme === null || saveRequest.current) return
    const controller = new AbortController()
    saveRequest.current = controller
    setSaving(true)
    setResult(null)
    try {
      const response = await request('/api/v1/admin/appearance', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ theme }), signal: controller.signal,
      })
      const value = await readTheme(response)
      if (controller.signal.aborted) return
      if (value !== theme) throw new Error('保存结果与所选配色不一致，请重试。')
      setSavedTheme(value)
      setResult({ success: true, message: '配色已保存到服务端 SQLite 数据库，下次打开管理后台会自动使用此配色。' })
    } catch (cause) {
      if (!controller.signal.aborted) setResult({ success: false, message: cause instanceof Error ? cause.message : '配色保存未完成，请重试。' })
    } finally {
      if (saveRequest.current === controller) { saveRequest.current = null; setSaving(false) }
    }
  }
  const retry = useCallback(() => setRevision(value => value + 1), [])
  const dismissResult = () => setResult(null)
  return { theme, savedTheme, loading, error, saving, result, select: setTheme, save, retry, dismissResult }
}
