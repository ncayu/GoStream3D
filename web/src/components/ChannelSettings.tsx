import { useEffect, useState, type RefObject } from 'react'
import { ConsoleIcon as Icon } from './ConsoleIcon'
import { fetchWithTimeout } from '../request'

type Settings = { max_channels: number; active_channels: number }
type Props = { refreshVersion: number; sectionRef: RefObject<HTMLElement>; onChanged?: () => void }

export function ChannelSettings({ refreshVersion, sectionRef, onChanged }: Props) {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [maxChannels, setMaxChannels] = useState(3)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [readError, setReadError] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    let active = true
    const controller = new AbortController()
    setLoading(true); setReadError(false); setError(''); setNotice('')
    void fetchWithTimeout('/api/v1/admin/channels/settings', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
      .then(async response => {
        const body = await response.json() as { data?: Settings; msg?: string }
        if (!response.ok || !body.data || !Number.isInteger(body.data.max_channels) || body.data.max_channels < 1 || body.data.max_channels > 100) throw new Error(body.msg || '读取渲染通道设置失败')
        if (active) { setSettings(body.data); setMaxChannels(body.data.max_channels) }
      })
      .catch(cause => { if (active) { setReadError(true); setError(cause instanceof Error ? cause.message : '读取渲染通道设置失败') } })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false; controller.abort() }
  }, [refreshVersion])

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    setError(''); setNotice('')
    if (!Number.isInteger(maxChannels) || maxChannels < 1 || maxChannels > 100) { setError('服务器总通道数须为 1–100 的整数。'); return }
    setSaving(true)
    try {
      const response = await fetchWithTimeout('/api/v1/admin/channels/settings', {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ max_channels: maxChannels }),
      })
      const body = await response.json() as { data?: Settings; msg?: string }
      if (!response.ok || !body.data || !Number.isInteger(body.data.max_channels) || body.data.max_channels < 1 || body.data.max_channels > 100) throw new Error(body.msg || '保存渲染通道设置失败')
      setSettings(body.data); setMaxChannels(body.data.max_channels)
      setNotice('已保存，总通道数立即生效，重启后继续保留。新增实例在访问时按需启动。')
      onChanged?.()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '保存渲染通道设置失败')
    } finally { setSaving(false) }
  }

  const disabled = loading || saving || readError
  const changed = settings !== null && maxChannels !== settings.max_channels
  const current = loading || readError || !settings ? '—' : settings.max_channels
  return <section className="console-panel settings-resource-card" ref={sectionRef} aria-labelledby="channel-settings-heading">
    <div className="resource-card-heading"><span className="metric-icon"><Icon name="users" size={29} /></span><div><h2 id="channel-settings-heading">渲染通道设置</h2><p>所有项目的 CPU / GPU 实例与预加载实例共同占用。</p></div><span className="soft-badge">当前 {current} 路</span></div>
    <form onSubmit={event => void save(event)}>
      <div className="monitor-sampling-controls"><div className="field settings-number-field"><label htmlFor="max-channels">服务器总通道数</label><div className="number-with-unit"><input id="max-channels" type="number" min={1} max={100} step={1} value={maxChannels} onChange={event => setMaxChannels(Number(event.target.value))} disabled={disabled} required /><span>路</span></div></div><div className="setting-presets" aria-label="常用总通道数">{[3, 5, 10, 20, 50, 100].map(value => <button key={value} type="button" className={maxChannels === value ? 'selected' : ''} aria-pressed={maxChannels === value} onClick={() => setMaxChannels(value)} disabled={disabled}>{value} 路</button>)}</div></div>
      <p className="setting-description">支持自定义 1–100 路。此值是配置额度，实际流畅度取决于硬件与场景。可在下方“实际场景容量测试”测量后再调整，并为预加载与其他项目预留资源。</p>
      <p className="channel-usage">已占用通道：<strong>{loading || readError || !settings ? '—' : settings.active_channels} / {current}</strong><span>包含无人观看时保留的预加载实例</span></p>
      <p className="setting-description">扩大配额会同步调整内部端口池。缩小时如有实例占用超出新范围的通道，请先停止相关渲染后重试。</p>
      {error && <div className="alert alert-error" role="alert">{error}</div>}{notice && <div className="alert alert-success" role="status">{notice}</div>}
      <div className="resource-save-bar"><span>{changed && !loading ? <><i className="unsaved-dot" />修改尚未保存</> : '设置保存在服务器，重启后保留。'}</span><button type="submit" className="primary-button" disabled={disabled || !changed}>{saving ? '保存中…' : '保存总通道数'}</button></div>
    </form>
  </section>
}
