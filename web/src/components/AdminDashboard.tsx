import { changeSessionMode, normalizeRouteCounts } from '../session-limits'
import { useCallback, useEffect, useRef, useState } from 'react'
import { ThemeSettings } from './ThemeSettings'
import { About } from './About'
import { PlatformMonitor } from './PlatformMonitor'
import { PlatformSettings } from './PlatformSettings'
import { StoreHealthBanner } from './StoreRecovery'
import { useAppearance } from '../appearance'
import { ConsoleIcon as Icon } from './ConsoleIcon'
import { CreateWorkspace } from './CreateWorkspace'
import { SessionQuotaField, PreloadCountField } from './SessionQuotaField'
import { ProjectManager } from './ProjectManager'
import { ConsoleToast, type CopyFeedback } from './ConsoleToast'
import { streamURL, type Session, type FormState, type ResourcePolicy } from './ConsoleTypes'
import { copyText } from '../clipboard'
import { fetchWithTimeout } from '../request'
import { ProjectPerformanceFields } from './ProjectPerformanceFields'
import { HelpTooltip } from './HelpTooltip'
import { AdminAccountDialog } from './AdminAccountDialog'
import { deleteProjects, type DeleteProgress } from '../project-deletion'

type Section = 'streams' | 'links' | 'monitor' | 'platform-settings' | 'settings' | 'about'


function sectionFromHash(): Section {
  const hash = window.location.hash.slice(1)
  return hash === 'streams' || hash === 'links' || hash === 'monitor' || hash === 'platform-settings' || hash === 'settings' || hash === 'about' ? hash : 'links'
}

const initialForm: FormState = {
	show_fps: true,
  frame_policy: 'interactive', reserved_preloads: 0,
  name: '', target_url: '', resolution: '1920x1080', fps: 30,
  render_mode: 'cpu', concurrency_mode: 'single', max_viewers: 1, preload_count: 1, ready_selector: '',
}
function EditSessionDialog({ session, transport, channelCapacity, onClose, onSave }: {
  session: Session
  transport?: string
  channelCapacity?: number
  onClose: () => void
  onSave: (values: FormState) => Promise<void>
}) {
  const [values, setValues] = useState<FormState>(() => normalizeRouteCounts({
	show_fps: session.show_fps !== false,
    frame_policy: session.frame_policy || 'interactive', reserved_preloads: session.reserved_preloads || 0,
    name: session.name, target_url: session.target_url, resolution: session.resolution,
    fps: session.fps, render_mode: session.render_mode, concurrency_mode: session.concurrency_mode,
    max_viewers: session.max_viewers, preload_count: session.preload_count || 1, ready_selector: session.ready_selector || '',
  }))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const firstField = useRef<HTMLInputElement>(null)
  const dialog = useRef<HTMLDivElement>(null)
  const updateValue = <K extends keyof FormState>(key: K, value: FormState[K]) => setValues(current => ({ ...current, [key]: value }))
  const changePerformance = (patch: Partial<FormState>) => setValues(current => normalizeRouteCounts({ ...current, ...patch }))

  useEffect(() => {
    firstField.current?.focus()
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = previousOverflow }
  }, [])

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape' && !saving) onClose()
    if (event.key !== 'Tab' || !dialog.current) return
    const focusable = Array.from(dialog.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled)'))
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
  }

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setError('')
    try {
      await onSave({ ...values, max_viewers: values.concurrency_mode === 'single' ? 1 : values.max_viewers })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '保存失败。')
      setSaving(false)
    }
  }

  return <div className="edit-overlay" onMouseDown={event => { if (event.target === event.currentTarget && !saving) onClose() }}>
    <div className="edit-dialog" ref={dialog} role="dialog" aria-modal="true" aria-labelledby="edit-dialog-title" onKeyDown={handleKeyDown}>
      <div className="edit-dialog-heading"><div><h2 id="edit-dialog-title">编辑观看链接</h2><p>保存后继续使用原观看地址。</p></div><button type="button" className="edit-close" onClick={onClose} disabled={saving} aria-label="关闭编辑弹框">×</button></div>
      <form onSubmit={event => void submit(event)}>
        <div className="field"><label htmlFor="edit-name">项目名称</label><input ref={firstField} id="edit-name" value={values.name} onChange={event => updateValue('name', event.target.value)} maxLength={100} /></div>
        <div className="field"><label htmlFor="edit-target">原始 3D 页面 URL <b>*</b></label><input id="edit-target" type="url" value={values.target_url} onChange={event => updateValue('target_url', event.target.value)} required /></div>
        <div className="field"><label>渲染资源</label><div className="option-grid"><button className={`option-card ${values.render_mode === 'cpu' ? 'selected' : ''}`} type="button" onClick={() => updateValue('render_mode', 'cpu')}><span className="option-icon">CPU</span><strong>CPU 软件渲染</strong></button><button className={`option-card ${values.render_mode === 'nvidia' ? 'selected' : ''}`} type="button" onClick={() => updateValue('render_mode', 'nvidia')}><span className="option-icon">GPU</span><strong>本机 GPU</strong></button></div></div>
        <div className="field"><label>观看并发</label><div className="segmented"><button type="button" aria-pressed={values.concurrency_mode === 'single'} className={values.concurrency_mode === 'single' ? 'active' : ''} onClick={() => setValues(current => changeSessionMode(current, 'single'))}>单路会话</button><button type="button" className={values.concurrency_mode === 'multi' ? 'active' : ''} aria-pressed={values.concurrency_mode === 'multi'} onClick={() => setValues(current => changeSessionMode(current, 'multi'))}>多路独立</button></div></div>
        {values.concurrency_mode === 'multi' && <SessionQuotaField id="edit-viewers" value={values.max_viewers} capacity={channelCapacity} onChange={value => changePerformance({ max_viewers: value })} onOpenSettings={onClose} />}
        {values.concurrency_mode === 'multi' && <PreloadCountField id="edit-preload" value={values.preload_count} max={values.max_viewers} onChange={value => changePerformance({ preload_count: value })} />}
        <ProjectPerformanceFields values={values} onChange={changePerformance} />
        <div className="two-fields"><div className="field"><label htmlFor="edit-resolution">分辨率</label><select id="edit-resolution" value={values.resolution} onChange={event => setValues(current => ({ ...current, resolution: event.target.value, fps: event.target.value === '2560x1440' ? 30 : current.fps }))}><option value="adaptive">自适应观看窗口（推荐）</option><option value="1280x720">1280 × 720</option><option value="1920x1080">1920 × 1080</option><option value="2560x1440">2560 × 1440</option></select></div><div className="field"><label htmlFor="edit-fps">帧率</label><select id="edit-fps" value={values.fps} onChange={event => updateValue('fps', Number(event.target.value))}><option value={30}>30 FPS</option><option value={60} disabled={values.resolution === '2560x1440'}>60 FPS</option><option value={90} disabled={values.resolution === '2560x1440' || transport !== 'jpeg'}>90 FPS</option><option value={120} disabled={values.resolution === '2560x1440' || transport !== 'jpeg'}>120 FPS</option></select></div></div>
            {values.resolution === 'adaptive' && <p className="field-hint">服务端按每位观看者的窗口比例渲染，窗口变化时同步调整，铺满画面。超大窗口会按比例限制渲染尺寸。</p>}
        <p className="edit-dialog-note">每位用户占用独立渲染实例；当前服务器总通道配置为 {channelCapacity ?? "—"} 路。修改渲染配置后会重新预加载；已停止的链接将保持停止。</p>
        {error && <div className="alert alert-error" role="alert">{error}</div>}
        <div className="edit-dialog-actions"><button type="button" className="quiet-button" onClick={onClose} disabled={saving}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? '正在保存…' : '保存修改'}</button></div>
      </form>
    </div>
  </div>
}

export function AdminDashboard() {
  const [authenticated, setAuthenticated] = useState(false)
  const [checkingAuth, setCheckingAuth] = useState(true)
  const [usernameInput, setUsernameInput] = useState('')
  const [passwordInput, setPasswordInput] = useState('')
  const [sessions, setSessions] = useState<Session[]>([])
  const [resources, setResources] = useState<ResourcePolicy | null>(null)
  const [form, setForm] = useState<FormState>(initialForm)
  const [loading, setLoading] = useState(false)
  const [fetching, setFetching] = useState(false)
  const refreshInFlight = useRef<AbortController | null>(null)
  const [backendOnline, setBackendOnline] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNoticeValue] = useState('')
  const noticeTimer = useRef<number | null>(null)
  const setNotice = useCallback((message: string) => {
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current)
    noticeTimer.current = null
    setNoticeValue(message)
    if (message) noticeTimer.current = window.setTimeout(() => {
      noticeTimer.current = null
      setNoticeValue('')
    }, 2000)
  }, [])
  useEffect(() => () => {
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current)
  }, [])
  const [copyFeedback, setCopyFeedback] = useState<CopyFeedback | null>(null)
  const copyAttempt = useRef(0)
  const dismissCopyFeedback = useCallback(() => setCopyFeedback(null), [])

  useEffect(() => {
    if (!authenticated) { copyAttempt.current += 1; setCopyFeedback(null) }
  }, [authenticated])
  const [editing, setEditing] = useState<Session | null>(null)
  const [section, setSection] = useState<Section>(sectionFromHash)
  const [collapsed, setCollapsed] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [refreshVersion, setRefreshVersion] = useState(0)
  const [adminName, setAdminName] = useState('管理员')
  const [accountOpen, setAccountOpen] = useState(false)
  const [loginNotice, setLoginNotice] = useState('')

  useEffect(() => { document.querySelector('.console-main')?.scrollTo(0, 0) }, [section])

  useEffect(() => {
    if (!drawerOpen) return
    const previous = document.activeElement as HTMLElement | null
    const sidebar = document.querySelector<HTMLElement>('.admin-sidebar')
    sidebar?.querySelector<HTMLButtonElement>('.sidebar-nav')?.focus()
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setDrawerOpen(false)
      if (event.key !== 'Tab' || !sidebar) return
      const items = Array.from(sidebar.querySelectorAll<HTMLElement>('button:not(:disabled), summary, details[open] a'))
        .filter(item => item.getClientRects().length && getComputedStyle(item).visibility !== 'hidden')
      if (event.shiftKey && document.activeElement === items[0]) { event.preventDefault(); items.at(-1)?.focus() }
      else if (!event.shiftKey && document.activeElement === items.at(-1)) { event.preventDefault(); items[0]?.focus() }
    }
    document.addEventListener('keydown', handleKey)
    return () => { document.removeEventListener('keydown', handleKey); previous?.focus() }
  }, [drawerOpen])

  const navigate = (next: Section) => {
    setSection(next)
    setDrawerOpen(false)
    window.location.hash = next
  }

  useEffect(() => {
    const syncSection = () => setSection(sectionFromHash())
    window.addEventListener('hashchange', syncSection)
    return () => window.removeEventListener('hashchange', syncSection)
  }, [])

  const request = useCallback(async (path: string, init: RequestInit = {}) => {
    const response = await fetchWithTimeout(path, {
      ...init,
      credentials: 'same-origin',
    })
    init.signal?.throwIfAborted()
    if (response.status === 401) {
      setAuthenticated(false)
      throw Object.assign(new Error('登录已失效，请重新登录。'), { status: response.status })
    }
    if (!response.ok) {
      const body = await response.json().catch(() => null) as { msg?: string } | null
      throw Object.assign(new Error(body?.msg || `请求失败（${response.status}）`), { status: response.status })
    }
    return response
  }, [])

  const appearance = useAppearance(authenticated, request)
  const theme = appearance.theme

  useEffect(() => {
    let active = true
    const controller = new AbortController()
    void fetchWithTimeout('/api/v1/auth/me', { credentials: 'same-origin', signal: controller.signal })
      .then(async response => { if (!active) return; setAuthenticated(response.ok); if (response.ok) { const body = await response.json() as { username?: string }; if (active && body.username) setAdminName(body.username) } })
      .catch(cause => { if (active) { setAuthenticated(false); setError(cause instanceof Error ? cause.message : '无法检查登录状态，请重试。') } })
      .finally(() => { if (active) setCheckingAuth(false) })
    return () => { active = false; controller.abort() }
  }, [])

  const refresh = useCallback(async () => {
    if (!authenticated || refreshInFlight.current) return
    const controller = new AbortController()
    refreshInFlight.current = controller
    setFetching(true)
    try {
      const [response, resourceResponse] = await Promise.all([
        request('/api/v1/admin/sessions', { signal: controller.signal }),
        request('/api/v1/admin/resources', { signal: controller.signal }),
      ])
      const body = await response.json() as { data: Session[] }
      const resourceBody = await resourceResponse.json() as { data: ResourcePolicy }
      if (controller.signal.aborted) return
      setSessions(body.data || [])
      setResources(resourceBody.data)
      setBackendOnline(true)
      setError('')
    } catch (cause) {
      if (controller.signal.aborted) return
      setBackendOnline(false)
      setError(cause instanceof Error ? cause.message : '加载会话失败。')
    } finally {
      controller.abort()
      if (refreshInFlight.current === controller) {
        refreshInFlight.current = null
        setFetching(false)
      }
    }
  }, [authenticated, request])

  useEffect(() => {
    if (!authenticated) return
    void refresh()
    const timer = window.setInterval(() => void refresh(), 15000)
    return () => {
      window.clearInterval(timer)
      refreshInFlight.current?.abort()
      refreshInFlight.current = null
      setFetching(false)
    }
  }, [authenticated, refresh])

  const login = async (event: React.FormEvent) => {
    event.preventDefault()
    setError('')
    try {
      const response = await fetchWithTimeout('/api/v1/auth/login', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: usernameInput.trim(), password: passwordInput }),
      })
      if (!response.ok) {
        const body = await response.json().catch(() => null) as { msg?: string } | null
        throw new Error(body?.msg || `登录失败（${response.status}）`)
      }
      setAdminName(usernameInput.trim())
      setLoginNotice('')
      setPasswordInput('')
      navigate('links')
      setAuthenticated(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '登录失败，请检查网关连接。')
    }
  }

  const refreshOrCancel = () => {
    if (refreshInFlight.current) {
      refreshInFlight.current.abort()
      setNotice('刷新已取消，可再次刷新。')
      return
    }
    setNotice('')
    void refresh()
    setRefreshVersion(value => value + 1)
  }

  const logout = async () => {
    try {
      const response = await fetchWithTimeout('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' })
      if (!response.ok) throw new Error(`退出登录失败（${response.status}）`)
      setAuthenticated(false)
      setSessions([])
      setResources(null)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '退出登录失败，请重试。')
    }
  }

  const create = async (event: React.FormEvent) => {
    event.preventDefault()
    setLoading(true)
    setError('')
    setNotice('')
    try {
      const response = await request('/api/v1/sessions?async=1', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...form, max_viewers: form.concurrency_mode === 'single' ? 1 : form.max_viewers }),
      })
      const body = await response.json() as { data: { token: string; stream_url: string } }
      const data = body.data
      setSessions(current => [{
        token: data.token, stream_url: data.stream_url, name: form.name,
        target_url: form.target_url, resolution: form.resolution, fps: form.fps,
        render_mode: form.render_mode, concurrency_mode: form.concurrency_mode,
        max_viewers: form.concurrency_mode === 'single' ? 1 : form.max_viewers,
        preload_count: form.preload_count,
		show_fps: form.show_fps,
        frame_policy: form.frame_policy, reserved_preloads: form.reserved_preloads,
        ready_selector: form.ready_selector, viewers: 0, running_instances: 0, running: false,
      }, ...current])
      setForm(initialForm)
      setNotice('项目已保存，系统正在预加载。状态变为“已预加载”后即可分享观看链接。')
      navigate('links')
      void refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '创建失败。')
    } finally {
      setLoading(false)
    }
  }

  const removeMany = async (tokens: string[], signal: AbortSignal, onProgress: (value: DeleteProgress) => void) => {
    setError('')
    setNotice('')
    const result = await deleteProjects(tokens, async token => {
      await request(`/api/v1/admin/sessions/${encodeURIComponent(token)}`, { method: 'DELETE', signal })
    }, signal, onProgress)
    // Discard any older polling response before applying deletion results.
    refreshInFlight.current?.abort()
    refreshInFlight.current = null
    const deleted = new Set(result.deleted)
    setSessions(current => current.filter(item => !deleted.has(item.token)))
    await refresh()
    if (!signal.aborted) {
      const remaining = result.failed.length + result.skipped.length
      setNotice(`已删除 ${result.deleted.length} 个项目${remaining ? `，${remaining} 个未删除，可重试` : '，相关渲染实例已回收'}。`)
    }
    return result
  }

  const saveEdit = async (values: FormState) => {
    if (!editing) return
    await request(`/api/v1/admin/sessions/${encodeURIComponent(editing.token)}`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(values),
    })
    const changedRender = editing.target_url !== values.target_url || editing.resolution !== values.resolution ||
      editing.fps !== values.fps || editing.render_mode !== values.render_mode ||
      (editing.ready_selector || '') !== values.ready_selector ||
      (editing.preload_count || 1) !== values.preload_count
    setEditing(null)
    setNotice(changedRender && !editing.render_paused ? '修改已保存，正在重新预加载。观看地址保持不变。' : '修改已保存，观看地址保持不变。')
    await refresh()
  }

  const warm = async (token: string) => {
	setError('')
	setNotice('正在重新预加载页面，请稍候。')
	try {
		await request(`/api/v1/admin/sessions/${encodeURIComponent(token)}/warm?async=1`, { method: 'POST' })
		setSessions(current => current.map(item => item.token === token ? { ...item, preload_error: undefined } : item))
		setNotice('已开始重新预加载；后台会显示就绪状态。')
		void refresh()
	} catch (cause) {
		setError(cause instanceof Error ? cause.message : '预加载失败。')
		setNotice('')
	}
  }

  const setProjectFPS = async (token: string, visible: boolean) => {
    setError('')
    try {
      await request(`/api/v1/admin/sessions/${encodeURIComponent(token)}/presentation`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ show_fps: visible }),
      })
      refreshInFlight.current?.abort()
      refreshInFlight.current = null
      setSessions(current => current.map(item => item.token === token ? { ...item, show_fps: visible } : item))
      await refresh()
      setNotice(visible ? '已开启观看页 FPS 显示。' : '已关闭观看页 FPS 显示。')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'FPS 显示设置保存失败，请重试。')
      throw cause
    }
  }

  const startRendering = async (token: string) => {
    setError('')
    setNotice('正在启动渲染并预加载页面。')
    try {
      await request(`/api/v1/admin/sessions/${encodeURIComponent(token)}/start?async=1`, { method: 'POST' })
      setSessions(current => current.map(item => item.token === token ? { ...item, render_paused: false, preload_error: undefined } : item))
      void refresh()
    } catch (cause) {
      setNotice('')
      setError(cause instanceof Error ? cause.message : '启动渲染失败。')
    }
  }

  const stopRendering = async (token: string) => {
    setError('')
    setNotice('')
    try {
      await request(`/api/v1/admin/sessions/${encodeURIComponent(token)}/stop`, { method: 'POST' })
      setSessions(current => current.map(item => item.token === token ? { ...item, render_paused: true, running: false, viewers: 0, preload_error: undefined } : item))
      setNotice('渲染已停止。链接配置和浏览器缓存仍保留，可随时重新启动。')
      void refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '停止渲染失败。')
    }
  }

  const copy = async (item: Session, kind: 'stream' | 'source' = 'stream') => {
    const url = kind === 'source' ? item.target_url : streamURL(item)
    const attempt = ++copyAttempt.current
    try {
      await copyText(url)
      if (attempt === copyAttempt.current) setCopyFeedback({ id: attempt, kind: 'success' })
    } catch {
      if (attempt === copyAttempt.current) setCopyFeedback({ id: attempt, kind: 'error' })
    }
  }

  if (checkingAuth) return <main className="admin-shell login-shell" data-theme={theme}><div className="login-panel">正在检查登录状态…</div></main>
  if (!authenticated) return <main className="admin-shell login-shell" data-theme={theme}><div className="login-panel"><div className="brand-mark">G<span>3</span></div><h1>GoStream3D</h1><p>登录云渲染管理平台</p>{loginNotice && <div className="alert alert-success" role="status">{loginNotice}</div>}<form onSubmit={event => void login(event)}><label htmlFor="admin-username">管理员账号</label><input id="admin-username" type="text" autoComplete="username" value={usernameInput} onChange={event => setUsernameInput(event.target.value)} placeholder="输入管理员账号" required /><label htmlFor="admin-password">管理员密码</label><input id="admin-password" type="password" autoComplete="current-password" value={passwordInput} onChange={event => setPasswordInput(event.target.value)} placeholder="输入管理员密码" required />{error && <div className="login-error" role="alert">{error}</div>}<button className="primary-button" type="submit">登录</button></form><small>初始账号见安装配置，登录后可在用户菜单修改账号密码。观看链接无需登录。</small></div></main>

  const sectionTitles: Record<Section, string> = { streams: '创建观看链接', links: '已创建的链接', monitor: '平台监控', 'platform-settings': '后台设置', settings: '外观设置', about: '关于' }
  const descriptions: Record<Section, string> = {
    streams: '配置 3D 页面与渲染参数，预加载后生成可分享的观看链接。',
    links: '管理云渲染项目、查看运行状态并分享观看地址。',
    monitor: '查看服务器的实时资源状态，掌握平台运行趋势。',
    'platform-settings': '配置渲染通道总配额、CPU 使用上限与监控采样策略。',
    settings: '为云渲染控制台选择清晰、舒适的界面配色。',
    about: '了解 GoStream3D 的用途、主要功能与使用场景。',
  }
  const isSystem = section === 'platform-settings' || section === 'settings' || section === 'about'
  const navItem = (id: Section, icon: string) => <button type="button" key={id} className={`sidebar-nav ${section === id ? 'sidebar-active' : ''}`} aria-current={section === id ? 'page' : undefined} title={collapsed ? sectionTitles[id] : undefined} onClick={() => navigate(id)}><span className="nav-icon"><Icon name={icon} size={21} /></span><span className="nav-label">{sectionTitles[id]}</span>{id === 'links' && <span className="nav-count">{sessions.length}</span>}</button>
  const accountMenu = <details className="account-menu"><summary aria-label={`用户菜单：${adminName}`}><span className="admin-avatar">{adminName.charAt(0).toUpperCase()}</span><span className="account-name" title={adminName}>{adminName}</span><Icon name="chevron" size={15} /></summary><div className="dropdown-menu"><span className="account-role">平台管理员</span><button type="button" onClick={e => { e.currentTarget.closest('details')?.removeAttribute('open'); setAccountOpen(true) }}><Icon name="user" size={17} />修改账号密码</button><button type="button" onClick={e => { e.currentTarget.closest('details')?.removeAttribute('open'); navigate('settings') }}><Icon name="palette" size={17} />外观设置</button><button type="button" onClick={() => void logout()}><Icon name="logout" size={17} />退出登录</button></div></details>

  return <main className={`admin-shell console-app ${collapsed ? 'sidebar-collapsed' : ''} ${drawerOpen ? 'mobile-drawer-open' : ''}`} data-theme={theme}>
    {copyFeedback && <ConsoleToast key={copyFeedback.id} feedback={copyFeedback} onDismiss={dismissCopyFeedback} />}
    {drawerOpen && <button className="sidebar-scrim" onClick={() => setDrawerOpen(false)} aria-label="关闭导航" />}
    <aside className="admin-sidebar">
      <div className="sidebar-brand"><div className="brand-mark">G3</div><div><strong>GoStream3D</strong><small>云渲染控制台</small></div></div>
      <nav className="sidebar-group" aria-label="工作空间"><span>工作空间</span>{navItem('links', 'link')}{navItem('streams', 'create-link')}{navItem('monitor', 'monitor')}</nav>
      <nav className="sidebar-group system-nav" aria-label="系统管理"><span>系统管理</span>{navItem('platform-settings', 'settings')}{navItem('settings', 'palette')}{navItem('about', 'info')}</nav>
      <div className="sidebar-bottom"><div className="sidebar-connection"><i className={`live-dot ${backendOnline ? '' : 'offline-dot'}`} /><span>{backendOnline ? '服务已连接' : '服务未连接'}</span></div></div>
    </aside>
    <div className="console-main">
      <div className="console-topbar"><div className="console-breadcrumb"><button className="icon-button desktop-menu-toggle" aria-label={collapsed ? '展开导航' : '收起导航'} aria-expanded={!collapsed} onClick={() => setCollapsed(value => !value)}><Icon name="menu" size={20} /></button><button className="icon-button mobile-menu-toggle" aria-label="打开导航" aria-expanded={drawerOpen} onClick={() => setDrawerOpen(value => !value)}><Icon name="menu" size={20} /></button><span>{isSystem ? '系统管理' : '工作空间'}</span><i>/</i><strong>{sectionTitles[section]}</strong></div><div className="console-topbar-actions"><span className={`server-online ${backendOnline ? '' : 'server-offline'}`}><i className="live-dot" />{backendOnline ? '渲染服务在线' : '服务未连接'}</span><button className="quiet-button refresh-console" type="button" onClick={refreshOrCancel}><Icon name="refresh" size={17} /><span>{fetching ? '取消刷新' : '刷新状态'}</span></button>{accountMenu}</div></div>
      <div className={`console-content ${section === 'streams' ? 'console-content-create' : ''}`}>
        <StoreHealthBanner refreshVersion={refreshVersion} />
        <header className="console-page-heading"><div><div className="console-heading-title"><h1>{sectionTitles[section]}</h1>{section === 'streams' && <HelpTooltip label="创建观看链接">{descriptions[section]}</HelpTooltip>}</div>{section !== 'streams' && <p>{descriptions[section]}</p>}</div>{section === 'streams' && <button className="quiet-button heading-action" type="button" onClick={() => navigate('links')}><Icon name="file" size={18} />查看已有链接<Icon name="arrow" size={16} /></button>}{section === 'links' && <button className="primary-button heading-action" type="button" onClick={() => navigate('streams')}><Icon name="plus" size={19} />创建新链接</button>}</header>
        {(error || notice) && <div className={`alert ${error ? 'alert-error' : 'alert-success'}`} role={error ? 'alert' : 'status'}><Icon name="info" size={19} /><span>{error || notice}</span><button type="button" onClick={() => { setError(''); setNotice('') }} aria-label="关闭提示"><Icon name="close" size={17} /></button></div>}
        {section === 'streams' && <CreateWorkspace values={form} setValues={setForm} resources={resources} loading={loading} onSubmit={create} onReset={() => { setForm(initialForm); setError(''); setNotice('') }} />}
        {section === 'about' && <About />}
        {section === 'links' && <ProjectManager sessions={sessions} capacity={resources?.channel_capacity} onCopy={copy} onEdit={setEditing} onStart={startRendering} onStop={stopRendering} onWarm={warm} onShowFPS={setProjectFPS} onDeleteMany={removeMany} onCreate={() => navigate('streams')} />}
        {section === 'monitor' && <PlatformMonitor theme={theme} refreshVersion={refreshVersion} />}
        {section === 'platform-settings' && <PlatformSettings onCPUChanged={() => void refresh()} refreshVersion={refreshVersion} />}
        {section === 'settings' && <ThemeSettings appearance={appearance} />}
        <footer className="console-footer"><span>GoStream3D · 服务端数字孪生云渲染</span><span>© 2026 GoStream3D</span></footer>
      </div>
    </div>
    {editing && <EditSessionDialog key={editing.token} session={editing} transport={resources?.transport} channelCapacity={resources?.channel_capacity} onClose={() => setEditing(null)} onSave={saveEdit} />}
    {accountOpen && <AdminAccountDialog username={adminName} onClose={() => setAccountOpen(false)} onSaved={username => {
      setAccountOpen(false); setAuthenticated(false); setSessions([]); setResources(null); setEditing(null)
      setUsernameInput(username); setPasswordInput(''); setError(''); setNotice('')
      setLoginNotice('账号密码已修改，请使用新账号重新登录。')
    }} />}
  </main>
}
