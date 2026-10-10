import { useEffect, useRef, useState } from 'react'
import { ConsoleIcon as Icon } from './ConsoleIcon'
import { ChannelSettings } from './ChannelSettings'
import { fetchWithTimeout } from '../request'
import { PerformanceSettings } from './StreamPerformance'
import { CapacityTest } from './CapacityTest'
import { StoreRecovery } from './StoreRecovery'

type Props = { onCPUChanged?: () => void; refreshVersion?: number }

export function PlatformSettings({ onCPUChanged, refreshVersion = 0 }: Props) {
  const [cpuPercent, setCPUPercent] = useState(50)
  const [savedCPUPercent, setSavedCPUPercent] = useState(50)
  const [cpuSupported, setCPUSupported] = useState(false)
  const [cpuLoading, setCPULoading] = useState(true)
  const [cpuSaving, setCPUSaving] = useState(false)
  const [cpuError, setCPUError] = useState('')
  const [cpuReadError, setCPUReadError] = useState(false)
  const [cpuNotice, setCPUNotice] = useState('')
  const [interval, setIntervalSeconds] = useState(30)
  const [savedInterval, setSavedInterval] = useState(30)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [monitorReadError, setMonitorReadError] = useState(false)
  const [notice, setNotice] = useState('')
  const [focusedSection, setFocusedSection] = useState<'cpu' | 'channels' | 'monitor'>('cpu')
  const cpuCard = useRef<HTMLElement>(null)
  const monitorCard = useRef<HTMLElement>(null)
  const channelsCard = useRef<HTMLElement>(null)

  useEffect(() => {
    let active = true
    const controller = new AbortController()
    setCPULoading(true); setLoading(true); setCPUError(''); setError('')
    setCPUReadError(false); setMonitorReadError(false)
    void fetchWithTimeout('/api/v1/admin/cpu/settings', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
      .then(async response => {
        const body = await response.json() as { data?: { cpu_max_percent: number; supported: boolean }; msg?: string }
        if (!response.ok || !body.data || !Number.isInteger(body.data.cpu_max_percent) || body.data.cpu_max_percent < 0 || body.data.cpu_max_percent > 100) throw new Error(body.msg || '读取 CPU 使用上限失败')
        if (active) { setSavedCPUPercent(body.data.cpu_max_percent); setCPUPercent(body.data.cpu_max_percent || 50); setCPUSupported(body.data.supported) }
      })
      .catch(cause => { if (active) { setCPUReadError(true); setCPUError(cause instanceof Error ? cause.message : '读取 CPU 使用上限失败') } })
      .finally(() => { if (active) setCPULoading(false) })
    void fetchWithTimeout('/api/v1/admin/monitor/settings', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
      .then(async response => {
        const body = await response.json() as { data?: { interval_seconds: number }; msg?: string }
        if (!response.ok || !body.data) throw new Error(body.msg || '读取后台设置失败')
        if (active) { setIntervalSeconds(body.data.interval_seconds); setSavedInterval(body.data.interval_seconds) }
      })
      .catch(cause => { if (active) { setMonitorReadError(true); setError(cause instanceof Error ? cause.message : '读取后台设置失败') } })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false; controller.abort() }
  }, [refreshVersion])

  const saveCPU = async (event: React.FormEvent) => {
    event.preventDefault()
    setCPUError(''); setCPUNotice('')
    if (!Number.isInteger(cpuPercent) || cpuPercent < 1 || cpuPercent > 100) { setCPUError('CPU 使用上限须为 1–100 的整数百分比。'); return }
    setCPUSaving(true)
    try {
      const response = await fetchWithTimeout('/api/v1/admin/cpu/settings', {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cpu_max_percent: cpuPercent }),
      })
      const body = await response.json() as { data?: { cpu_max_percent: number }; msg?: string }
      if (!response.ok || !body.data || !Number.isInteger(body.data.cpu_max_percent) || body.data.cpu_max_percent < 1 || body.data.cpu_max_percent > 100) throw new Error(body.msg || '保存 CPU 使用上限失败')
      setCPUPercent(body.data.cpu_max_percent); setSavedCPUPercent(body.data.cpu_max_percent)
      setCPUNotice('已保存，CPU 使用上限立即生效，重启后继续保留。')
      onCPUChanged?.()
    } catch (cause) {
      setCPUError(cause instanceof Error ? cause.message : '保存 CPU 使用上限失败')
    } finally { setCPUSaving(false) }
  }

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    setError('')
    setNotice('')
    if (!Number.isInteger(interval) || interval < 10 || interval > 300) {
      setError('采样间隔须为 10–300 秒。')
      return
    }
    setSaving(true)
    try {
      const response = await fetchWithTimeout('/api/v1/admin/monitor/settings', {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ interval_seconds: interval }),
      })
      const body = await response.json() as { data?: { interval_seconds: number }; msg?: string }
      if (!response.ok || !body.data) throw new Error(body.msg || '保存失败')
      setIntervalSeconds(body.data.interval_seconds)
      setSavedInterval(body.data.interval_seconds)
      setNotice('已保存，新的采样间隔立即生效。')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const cpuDisabled = cpuLoading || cpuSaving || !cpuSupported
  const cpuChanged = cpuPercent !== savedCPUPercent
  const monitorChanged = interval !== savedInterval
  const currentCPU = cpuLoading || cpuReadError ? '—' : savedCPUPercent > 0 ? savedCPUPercent + '%' : '未设置'
  const selectSection = (next: 'cpu' | 'channels' | 'monitor') => {
    setFocusedSection(next)
    ;(next === 'cpu' ? cpuCard : next === 'channels' ? channelsCard : monitorCard).current?.scrollIntoView({ block: 'nearest', behavior: 'instant' })
  }
  return <section className="platform-settings-workspace" aria-label="后台资源与采样设置">
    <div className="settings-section-switch"><button type="button" className={focusedSection === 'cpu' ? 'active' : ''} aria-pressed={focusedSection === 'cpu'} onClick={() => selectSection('cpu')}><Icon name="cpu" size={18} />CPU 资源</button><button type="button" className={focusedSection === 'channels' ? 'active' : ''} aria-pressed={focusedSection === 'channels'} onClick={() => selectSection('channels')}><Icon name="users" size={18} />会话通道</button><button type="button" className={focusedSection === 'monitor' ? 'active' : ''} aria-pressed={focusedSection === 'monitor'} onClick={() => selectSection('monitor')}><Icon name="monitor" size={18} />监控采样</button></div>
    <div className="settings-workspace-grid"><div className="settings-card-stack">
      <section className="console-panel settings-resource-card" ref={cpuCard} aria-labelledby="platform-settings-heading">
        <div className="resource-card-heading"><span className="metric-icon"><Icon name="cpu" size={29} /></span><div><h2 id="platform-settings-heading">CPU 资源设置</h2><p>所有渲染实例与平台服务共享 CPU 配额。</p></div><span className="soft-badge">当前上限 {currentCPU}</span></div>
        <form onSubmit={event => void saveCPU(event)}>
          <div className="field settings-number-field"><label htmlFor="cpu-max-percent">本软件 CPU 使用上限</label><div className="number-with-unit"><input id="cpu-max-percent" type="number" min={1} max={100} step={1} value={cpuPercent} onChange={event => setCPUPercent(Number(event.target.value))} disabled={cpuDisabled} required /><span>%</span></div></div>
          <input className="cpu-quota-slider" type="range" aria-label="调整 CPU 使用上限" min={1} max={100} step={1} value={Math.max(1, Math.min(100, cpuPercent))} onChange={event => setCPUPercent(Number(event.target.value))} disabled={cpuDisabled} style={{ '--range-percent': Math.max(0, Math.min(100, (cpuPercent - 1) / 99 * 100)) + '%' } as React.CSSProperties} />
          <div className="range-markers"><span>1%</span><span>25%</span><span>50%</span><span>75%</span><span>100%</span></div>
          <div className="setting-presets" aria-label="常用 CPU 使用上限">{[25, 40, 50, 75].map(value => <button key={value} type="button" className={cpuPercent === value ? 'selected' : ''} aria-pressed={cpuPercent === value} onClick={() => setCPUPercent(value)} disabled={cpuDisabled}>{value}%</button>)}</div>
          <p className="setting-description">按整机 CPU 总能力计算，范围 1–100%，默认 50%。降低上限可能影响渲染帧率。其他软件不受此配额约束。</p>
          {!cpuLoading && !cpuSupported && !cpuError && <p className="console-note"><Icon name="info" size={18} />当前节点未启用 CPU 配额。Linux Docker 部署请使用新版 start.sh 配置共享 CPU 控制组；原生 Linux 需提供可写的 cgroup v2 CPU 控制组。</p>}
          {cpuError && <div className="alert alert-error" role="alert">{cpuError}</div>}{cpuNotice && <div className="alert alert-success" role="status">{cpuNotice}</div>}
          <div className="resource-save-bar"><span>{cpuChanged && !cpuLoading ? <><i className="unsaved-dot" />修改尚未保存</> : <><i className="live-dot" />保存后立即应用，无需重启。</>}</span><button type="submit" className="primary-button" disabled={cpuDisabled || !cpuChanged}>{cpuSaving ? '保存中…' : '保存 CPU 上限'}</button></div>
        </form>
      </section>
      <ChannelSettings refreshVersion={refreshVersion} sectionRef={channelsCard} onChanged={onCPUChanged} />
      <PerformanceSettings refreshVersion={refreshVersion} />
      <CapacityTest refreshVersion={refreshVersion} />
      <StoreRecovery refreshVersion={refreshVersion} />
      <section className="console-panel settings-resource-card" ref={monitorCard} aria-labelledby="monitor-settings-heading">
        <div className="resource-card-heading"><span className="metric-icon"><Icon name="monitor" size={29} /></span><div><h2 id="monitor-settings-heading">服务器监控设置</h2><p>控制 CPU、内存、磁盘和网络指标的采样频率。</p></div><span className="soft-badge">当前 {loading || monitorReadError ? '—' : savedInterval} 秒 / 次</span></div>
        <form onSubmit={event => void save(event)}>
          <div className="monitor-sampling-controls"><div className="field settings-number-field"><label htmlFor="monitor-interval">采样间隔</label><div className="number-with-unit"><input id="monitor-interval" type="number" min={10} max={300} step={1} value={interval} onChange={event => setIntervalSeconds(Number(event.target.value))} disabled={loading || saving} required /><span>秒</span></div></div><div className="setting-presets" aria-label="常用采样间隔">{[30, 60, 120, 300].map(value => <button key={value} type="button" className={interval === value ? 'selected' : ''} aria-pressed={interval === value} onClick={() => setIntervalSeconds(value)} disabled={loading || saving}>{value === 300 ? '5 分钟' : value + ' 秒'}</button>)}</div></div>
          <p className="setting-description">默认 30 秒，支持 10–300 秒。间隔越短，采集频率与开销越高。历史趋势保留近 1 小时。</p>
          <div className="monitor-scope"><strong>监控指标范围</strong>{[['cpu', 'CPU'], ['memory', '内存'], ['disk', '磁盘'], ['server', '网络']].map(([icon, text]) => <span key={text}><Icon name={icon} size={19} />{text}</span>)}</div>
          {error && <div className="alert alert-error" role="alert">{error}</div>}{notice && <div className="alert alert-success" role="status">{notice}</div>}
          <div className="resource-save-bar"><span>{monitorChanged && !loading ? <><i className="unsaved-dot" />修改尚未保存</> : '设置保存在服务器，重启后保留。'}</span><button type="submit" className="primary-button" disabled={loading || saving || !monitorChanged}>{saving ? '保存中…' : '保存采样设置'}</button></div>
        </form>
      </section>
    </div><aside className="console-panel settings-summary-panel">
      <div className="resource-card-heading"><span className="metric-icon"><Icon name="file" size={28} /></span><div><h2>当前配置摘要</h2><p>已生效的后台设置，非实时资源使用情况。</p></div></div>
      <div className="settings-limit-value" aria-label={'当前 CPU 配额上限 ' + currentCPU}><span>本软件 CPU 使用上限</span><strong>{currentCPU}</strong><small>默认 50% · 保存后立即生效</small></div>
      <dl className="console-summary"><div><dt>监控采样间隔</dt><dd>{loading || monitorReadError ? '—' : savedInterval + ' 秒 / 次'}</dd></div><div><dt>CPU 配额作用范围</dt><dd>全部渲染实例与平台服务</dd></div><div><dt>配置生效方式</dt><dd>保存后立即应用</dd></div></dl>
      <h3>调整说明</h3><div className="settings-explanation">{[['users', '通道配额', '服务器总通道数由管理员配置，预加载实例也占用通道。项目最大会话数可单独设置。'], ['cpu', 'CPU 配额共享', '全部 CPU / GPU 渲染实例与平台服务共用当前软件配额。'], ['monitor', '渲染性能', '实际帧率取决于 CPU 配额、场景复杂度与服务器算力。'], ['clock', '监控采样', '采样越频繁，趋势越细致，同时增加系统采集开销。']].map(([icon, title, text]) => <div key={title}><Icon name={icon} size={24} /><strong>{title}</strong><p>{text}</p></div>)}</div>
      <p className="console-note"><Icon name="info" />CPU 配额是使用上限，不代表当前 CPU 使用率。</p>
    </aside></div>
  </section>
}

