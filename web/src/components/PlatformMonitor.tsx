import { Component, Suspense, lazy, useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import type { MonitorPoint } from './MonitorTrendChart'
import { fetchWithTimeout } from '../request'
import { StreamPerformancePanel } from './StreamPerformance'
import { GPUMonitor, type GPU } from './GPUMonitor'

const MonitorTrendChart = lazy(() => import('./MonitorTrendChart'))

type Resource = { used_percent: number; used_bytes: number; total_bytes: number }
type Partition = { mountpoint: string; device: string; fs_type: string; used_bytes: number; total_bytes: number; percent: number }
type NetworkInterface = { name: string; up: boolean; ipv4?: string; rx_mbps: number; tx_mbps: number }
type Snapshot = {
  gpus?: GPU[]
  gpu_message?: string
  sampled_at: string
  interval_seconds: number
  cpu_percent: number
  cpu_cores: number
  memory: Resource
  disk: Resource
  partitions: Partition[]
  rx_mbps: number
  tx_mbps: number
  interfaces: NetworkInterface[]
  history: MonitorPoint[]
  system: { os: string; platform: string; version: string; kernel: string; hostname: string; uptime_seconds: number; local_ip: string }
  errors: string[]
}

const percent = (value: number) => `${Number.isFinite(value) ? value.toFixed(1) : '0.0'}%`
const bytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}
const mbps = (value: number) => `${value < 0.01 ? value.toFixed(3) : value.toFixed(2)} Mbps`
const time = (value: string) => value ? new Date(value).toLocaleTimeString('zh-CN', { hour12: false }) : '—'
const uptime = (seconds: number) => `${Math.floor(seconds / 86400)} 天 ${Math.floor(seconds % 86400 / 3600)} 小时`

function Gauge({ label, value, detail, tone }: { label: string; value: number; detail: string; tone: string }) {
  return <article className="monitor-metric">
    <span>{label}</span>
    <div className="monitor-metric-value"><strong>{percent(value)}</strong><small>{detail}</small></div>
    <div className="monitor-progress" role="progressbar" aria-label={label} aria-valuenow={Math.round(value)} aria-valuemin={0} aria-valuemax={100}><i style={{ width: `${Math.min(100, Math.max(0, value))}%`, background: tone }} /></div>
  </article>
}

class MonitorErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() {
    return this.state.failed
      ? <section className="monitor-shell"><div className="monitor-empty" role="alert">监控页面暂时无法显示。请刷新页面重试；服务器采样仍会继续。</div></section>
      : this.props.children
  }
}

export function PlatformMonitor({ theme, refreshVersion = 0 }: { theme: string; refreshVersion?: number }) {
  return <MonitorErrorBoundary><PlatformMonitorContent theme={theme} refreshVersion={refreshVersion} /></MonitorErrorBoundary>
}

function PlatformMonitorContent({ theme, refreshVersion }: { theme: string; refreshVersion: number }) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [error, setError] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const [pollSeconds, setPollSeconds] = useState(30)
  const manualRequest = useRef<AbortController | null>(null)
  const load = useCallback(async (force = false, signal?: AbortSignal) => {
    const response = await fetchWithTimeout('/api/v1/admin/monitor' + (force ? '/refresh' : ''), {
      method: force ? 'POST' : 'GET', credentials: 'same-origin', cache: 'no-store', signal,
    })
    const body = await response.json() as { data?: Snapshot; msg?: string }
    if (!response.ok || !body.data) throw new Error(body.msg || `监控数据读取失败（${response.status}）`)
    if (signal?.aborted) return
    setSnapshot({ ...body.data, errors: body.data.errors ?? [], partitions: body.data.partitions ?? [], interfaces: body.data.interfaces ?? [], history: body.data.history ?? [] })
    setPollSeconds(body.data.interval_seconds || 30)
    setError('')
  }, [])

  useEffect(() => {
    let active = true
    const controller = new AbortController()
    const poll = () => { void load(false, controller.signal).catch(cause => { if (active) setError(cause instanceof Error ? cause.message : '监控数据读取失败') }) }
    poll()
    const timer = window.setInterval(poll, pollSeconds * 1000)
    return () => { active = false; controller.abort(); window.clearInterval(timer) }
  }, [load, pollSeconds, refreshVersion])

  useEffect(() => () => { manualRequest.current?.abort(); manualRequest.current = null }, [])

  const refresh = async () => {
    if (manualRequest.current) return
    const controller = new AbortController()
    manualRequest.current = controller
    setRefreshing(true)
    try { await load(true, controller.signal) }
    catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '刷新失败') }
    finally { if (manualRequest.current === controller) { manualRequest.current = null; setRefreshing(false) } }
  }

  if (!snapshot) return <section className="monitor-shell"><div className="monitor-toolbar"></div><div className="monitor-empty">{error || '正在读取服务器指标…'}</div></section>
  const system = snapshot.system
  return <section className="monitor-shell" aria-label="平台监控">
    <StreamPerformancePanel refreshVersion={refreshVersion} />
    <GPUMonitor cards={snapshot.gpus} message={snapshot.gpu_message} history={snapshot.history} theme={theme} intervalSeconds={snapshot.interval_seconds} />
    <div className="monitor-toolbar"><div><p>本页展示运行 GoStream3D 的服务器指标，每 {snapshot.interval_seconds} 秒采样一次。可在“后台设置”中调整。</p></div><div className="monitor-toolbar-actions"><span className={`monitor-health ${snapshot.errors.length ? 'monitor-health-warn' : ''}`}>{snapshot.errors.length ? '部分指标不可用' : '运行正常'}</span><small>最近更新 {time(snapshot.sampled_at)}</small><button type="button" className="quiet-button" onClick={() => void refresh()} disabled={refreshing}>{refreshing ? '刷新中…' : '刷新'}</button></div></div>
    {error && <div className="alert alert-error" role="alert">{error}</div>}
    {snapshot.errors.length > 0 && <div className="monitor-warning" role="status">{snapshot.errors.join('；')}</div>}
    <div className="monitor-metrics">
      <Gauge label="整机 CPU 使用率" value={snapshot.cpu_percent} detail={`${snapshot.cpu_cores} 个逻辑核心 · 含其他进程`} tone="var(--accent)" />
      <Gauge label="内存使用率" value={snapshot.memory.used_percent} detail={`${bytes(snapshot.memory.used_bytes)} / ${bytes(snapshot.memory.total_bytes)}`} tone="var(--success)" />
      <Gauge label="磁盘使用率" value={snapshot.disk.used_percent} detail={`${bytes(snapshot.disk.used_bytes)} / ${bytes(snapshot.disk.total_bytes)}`} tone="var(--accent)" />
      <article className="monitor-metric monitor-network-metric"><span>网络流量</span><div className="monitor-network-values"><strong>↓ {mbps(snapshot.rx_mbps)}</strong><strong>↑ {mbps(snapshot.tx_mbps)}</strong></div><small>当前服务器所有已连接接口</small></article>
    </div>
    <div className="monitor-grid monitor-grid-detail">
      <section className="monitor-panel"><div className="monitor-panel-heading"><div><h2>CPU 使用率趋势</h2><small>近 1 小时 · 整机使用率，含其他进程</small></div></div><Suspense fallback={<div className="monitor-empty">正在加载图表…</div>}><MonitorTrendChart history={snapshot.history} theme={theme} metric="cpu" /></Suspense></section>
      <section className="monitor-panel"><div className="monitor-panel-heading"><div><h2>内存使用率趋势</h2><small>近 1 小时 · 整机内存使用率</small></div></div><Suspense fallback={<div className="monitor-empty">正在加载图表…</div>}><MonitorTrendChart history={snapshot.history} theme={theme} metric="memory" /></Suspense></section>
    </div>
    <div className="monitor-grid monitor-grid-main">
      <section className="monitor-panel"><div className="monitor-panel-heading"><div><h2>网络流量趋势</h2><small>近 1 小时 · Mbps</small></div><div className="monitor-legend"><span className="rx">接收流量</span><span className="tx">发送流量</span></div></div><Suspense fallback={<div className="monitor-empty">正在加载图表…</div>}><MonitorTrendChart history={snapshot.history} theme={theme} /></Suspense></section>
      <section className="monitor-panel"><div className="monitor-panel-heading"><h2>系统信息</h2></div><dl className="monitor-info"><div><dt>操作系统</dt><dd>{[system.platform || system.os, system.version].filter(Boolean).join(' ') || '—'}</dd></div><div><dt>内核版本</dt><dd>{system.kernel || '—'}</dd></div><div><dt>主机名</dt><dd>{system.hostname || '—'}</dd></div><div><dt>运行时长</dt><dd>{uptime(system.uptime_seconds)}</dd></div><div><dt>内网地址</dt><dd>{system.local_ip || '—'}</dd></div></dl></section>
    </div>
    <div className="monitor-grid monitor-grid-detail">
      <section className="monitor-panel"><div className="monitor-panel-heading"><h2>磁盘分区</h2><small>{snapshot.partitions.length} 个分区</small></div><div className="monitor-partitions">{snapshot.partitions.length ? snapshot.partitions.map(partition => <div className="monitor-partition" key={partition.mountpoint}><strong title={partition.device}>{partition.mountpoint}</strong><div className="monitor-progress"><i style={{ width: `${Math.min(100, partition.percent)}%`, background: partition.percent >= 90 ? 'var(--danger)' : 'var(--accent)' }} /></div><span>{percent(partition.percent)}</span><small>{bytes(partition.used_bytes)} / {bytes(partition.total_bytes)}</small></div>) : <p className="monitor-empty">暂无可读取的磁盘分区</p>}</div></section>
      <section className="monitor-panel"><div className="monitor-panel-heading"><h2>网络接口</h2><small>{snapshot.interfaces.length} 个接口</small></div><div className="monitor-table-wrap"><table className="monitor-table"><thead><tr><th>接口名称</th><th>状态 / 地址</th><th>接收速率</th><th>发送速率</th></tr></thead><tbody>{snapshot.interfaces.map(item => <tr key={item.name}><td title={item.name}>{item.name}</td><td><span className={item.up ? 'monitor-connected' : 'monitor-disconnected'}>{item.up ? '已连接' : '未连接'}</span>{item.ipv4 && <small>{item.ipv4}</small>}</td><td>{mbps(item.rx_mbps)}</td><td>{mbps(item.tx_mbps)}</td></tr>)}</tbody></table>{snapshot.interfaces.length === 0 && <p className="monitor-empty">暂无网络接口数据</p>}</div></section>
    </div>
  </section>
}
