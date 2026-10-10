import { useEffect, useState, type FormEvent } from 'react'
import { fetchWithTimeout } from '../request'
import type { WebRTCPerformance } from '../webrtc-performance'

type Settings = { gpu_fps_budget: number; reclaim_idle_preloads: boolean }
type Stream = {
  viewer_id?: string; transport?: string; webrtc?: WebRTCPerformance; project_name?: string
  token: string; project_token: string; render_mode: string; frame_policy: string; watching: boolean
  target_fps: number; budget_fps: number; capture_fps: number; display_fps: number; frame_bytes: number
  snapshot_capture_encode_ms: number; capture_delivery_ms: number; publish_ms: number; write_ms: number
  frame_ack_ms: number; decode_ms: number; draw_ms: number; server_dropped: number; client_dropped: number
  feedback_at: string
  render_fps: number; send_fps: number; input_frame_ms: number; input_sample_at: string
}
type PerformanceData = { settings: Settings; streams: Stream[]; jpeg_metrics_supported: boolean; webrtc_metrics_supported?: boolean }

async function readPerformance(signal: AbortSignal): Promise<PerformanceData> {
  const response = await fetchWithTimeout('/api/v1/admin/performance', { credentials: 'same-origin', cache: 'no-store', signal })
  const body = await response.json() as { data?: PerformanceData; msg?: string }
  if (!response.ok || !body.data) throw new Error(body.msg || '读取性能策略失败，请刷新重试。')
  return body.data
}

export function PerformanceSettings({ refreshVersion = 0 }: { refreshVersion?: number }) {
  const [settings, setSettings] = useState<Settings>({ gpu_fps_budget: 120, reclaim_idle_preloads: true })
  const [saved, setSaved] = useState<Settings | null>(null)
  const [supported, setSupported] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    void readPerformance(controller.signal).then(data => {
      if (!controller.signal.aborted) { setSettings(data.settings); setSaved(data.settings); setSupported(data.jpeg_metrics_supported); setError('') }
    }).catch(cause => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '读取失败') })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [refreshVersion])
  const save = async (event: FormEvent) => {
    event.preventDefault(); setSaving(true); setError(''); setNotice('')
    try {
      const response = await fetchWithTimeout('/api/v1/admin/performance/settings', {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings),
      })
      const body = await response.json() as { data?: Settings; msg?: string }
      if (!response.ok || !body.data) throw new Error(body.msg || '保存性能策略失败')
      setSettings(body.data); setSaved(body.data); setNotice('已保存，新的策略立即用于后续调度与观看请求。')
    } catch (cause) { setError(cause instanceof Error ? cause.message : '保存失败') }
    finally { setSaving(false) }
  }
  const changed = !!saved && (settings.gpu_fps_budget !== saved.gpu_fps_budget || settings.reclaim_idle_preloads !== saved.reclaim_idle_preloads)
  return <section className="console-panel settings-resource-card" aria-labelledby="performance-settings-title">
    <div className="resource-card-heading"><div><h2 id="performance-settings-title">多路性能与空闲通道</h2><p>分配 GPU 画面更新预算，并为实际观看请求让出空闲资源。</p></div></div>
    <form onSubmit={event => void save(event)}>
      <div className="field"><label htmlFor="gpu-budget">GPU 总帧率预算（所有 GPU 路共享）</label><input id="gpu-budget" type="number" min={30} max={1200} step={1} value={settings.gpu_fps_budget} disabled={loading || saving || !supported} onChange={event => setSettings(current => ({ ...current, gpu_fps_budget: Number(event.target.value) }))} required /></div>
      <p className="setting-description">默认 120。优先分配正在交互与持续动画的画面，再按各路帧率和延迟调整。此值是调度预算，实际流畅度仍取决于场景、硬件与网络。</p>
      {!loading && !supported && <p className="console-note">当前节点未使用 Windows JPEG 传输，GPU 总帧率预算不适用。</p>}
      <div className="field"><label htmlFor="reclaim-preloads">通道不足时的空闲实例策略</label><select id="reclaim-preloads" value={String(settings.reclaim_idle_preloads)} disabled={loading || saving} onChange={event => setSettings(current => ({ ...current, reclaim_idle_preloads: event.target.value === 'true' }))}>
        <option value="true">为观看请求回收其他项目的空闲预加载</option><option value="false">保留所有预加载实例</option>
      </select></div>
      <p className="setting-description">回收仅在实际观看请求遇到通道不足时触发。重点项目可在项目配置中设置“优先保留预加载路数”。回收后的实例至少等待 1 分钟，且有空闲通道时才恢复预加载。</p>
      {error && <div className="alert alert-error" role="alert">{error}</div>}{notice && <div className="alert alert-success" role="status">{notice}</div>}
      <div className="resource-save-bar"><span>配置保存在服务器，重启后保留。</span><button className="primary-button" type="submit" disabled={loading || saving || !changed}>{saving ? '保存中…' : '保存性能策略'}</button></div>
    </form>
  </section>
}

const number = (value: number) => Number.isFinite(value) ? value.toFixed(1) : '—'
const ms = (value: number) => value > 0 ? `${number(value)} ms` : '—'
const measured = (value: number | null | undefined, unit = '') => value !== null && value !== undefined && Number.isFinite(value) ? `${number(value)}${unit}` : '—'
function webRTCObservation(stream: Stream) {
  const m = stream.webrtc
  if (!m) return !stream.feedback_at || stream.feedback_at.startsWith('0001') ? '等待观看端采样' : '采样已过期'
  if (m.packets_lost !== null && m.packets_lost > 0) return '采样期间存在丢包'
  if (m.decode_ms !== null && m.decode_ms > 1000 / stream.target_fps) return '客户端解码耗时偏高'
  if (m.buffer_ms !== null && m.buffer_ms > 100) return '播放缓冲偏高'
  if (m.display_fps !== null && m.receive_fps !== null && m.receive_fps - m.display_fps > 3) return '实收与显示帧率有差距'
  return '持续测量中'
}
function observation(stream: Stream) {
  if (!stream.watching) return '无人观看'
  if (stream.write_ms > 50) return '发送存在等待'
  if (stream.decode_ms > 1000 / stream.target_fps) return '客户端解码耗时偏高'
  if (stream.frame_ack_ms > 250) return '显示反馈较慢'
  return stream.feedback_at.startsWith('0001') ? '等待客户端反馈' : '持续测量中'
}

export function StreamPerformancePanel({ refreshVersion = 0 }: { refreshVersion?: number }) {
  const [data, setData] = useState<PerformanceData | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const controller = new AbortController()
    let inFlight = false
    const poll = async () => {
      if (inFlight || document.visibilityState === 'hidden') return
      inFlight = true
      try { const result = await readPerformance(controller.signal); if (!controller.signal.aborted) { setData(result); setError('') } }
      catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '读取性能数据失败') }
      finally { inFlight = false }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), 3000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [refreshVersion])
  return <section className="console-panel project-management stream-performance" aria-labelledby="stream-performance-title">
    <div className="project-panel-title"><h2 id="stream-performance-title">逐路画面性能</h2><span className="soft-badge">{data?.streams.length ?? 0} 路 · 3 秒更新</span></div>
    {error && <div className="alert alert-error" role="alert">{error}</div>}
    {data?.webrtc_metrics_supported ? <>
      <p className="setting-description">Linux WebRTC 逐路测量。每条观看连接独立采样，支持 GPU / CPU；帧率与码率来自实际视频接收，后台每 3 秒更新。</p>
      <div className="project-table-wrap"><table className="project-table"><thead><tr><th>项目 / 实例 / 观看端</th><th>渲染模式</th><th>目标 FPS</th><th>实收 / 显示 FPS</th><th>分辨率 / 编码</th><th>视频码率</th><th>解码耗时</th><th>播放缓冲</th><th>网络往返 RTT</th><th>丢包 / 解码丢帧</th><th>样本时长</th><th>观测</th></tr></thead>
        <tbody>{data.streams.map(stream => <tr key={`${stream.token}:${stream.viewer_id}`}>
          <td title={`${stream.project_token} / ${stream.token}`}>{stream.project_name || stream.project_token.slice(-12)}<br />{stream.token.slice(-12)} · 观看端 {stream.viewer_id}</td>
          <td>{stream.render_mode === 'cpu' ? 'CPU' : 'GPU'}</td><td>{number(stream.target_fps)}</td>
          <td>{measured(stream.webrtc?.receive_fps)} / {measured(stream.webrtc?.display_fps)}</td>
          <td>{stream.webrtc ? `${stream.webrtc.width} × ${stream.webrtc.height} / ${stream.webrtc.codec.replace('video/', '') || '—'}` : '—'}</td>
          <td>{measured(stream.webrtc?.bitrate_mbps, ' Mbps')}</td><td>{measured(stream.webrtc?.decode_ms, ' ms')}</td>
          <td>{measured(stream.webrtc?.buffer_ms, ' ms')}</td><td>{measured(stream.webrtc?.rtt_ms, ' ms')}</td>
          <td>{measured(stream.webrtc?.packets_lost)} / {measured(stream.webrtc?.frames_dropped)}</td>
          <td>{measured(stream.webrtc?.sample_seconds, ' 秒')}</td><td>{webRTCObservation(stream)}</td>
        </tr>)}</tbody></table></div>
      {!data.streams.length && <p className="setting-description">暂无观看连接。打开观看链接，保持页面可见约 6 秒后可查看首个区间样本。</p>}
      <p className="setting-description">码率为视频 RTP 载荷；解码和缓冲为本次采样区间平均值，丢包与解码丢帧为区间增量。显示 FPS 为浏览器视频播放统计，不代表物理屏幕刷新率。RTT 为选中 ICE 链路的网络往返时间，不等于输入到画面的延迟。</p>
      <p className="setting-description">数据由观看端回传；不支持的指标显示“—”，真实零值保留。隐藏观看页暂停采样，超过 10 秒未收到有效反馈时清除旧值，断开连接后移除对应行。GPU 整机占用不能直接归属于单路。</p>
    </> : data?.jpeg_metrics_supported ? <>
      <p className="setting-description">GPU 总调度预算 {data.settings.gpu_fps_budget} FPS。采集、显示帧率与发送等待需要结合查看；GPU 使用率无法单独说明瓶颈。</p>
      <div className="project-table-wrap"><table className="project-table"><thead><tr><th>实例 / 策略</th><th>目标 / 调度 FPS</th><th>页面回调 FPS</th><th>采集 / 发送 / 显示 FPS</th><th>帧大小</th><th>截图 / 采集交付</th><th>服务端整理 / 发送</th><th>解码 / 绘制</th><th>显示反馈往返</th><th>输入至后续画面</th><th>服务端 / 客户端丢帧</th><th>观测</th></tr></thead>
        <tbody>{data.streams.map(stream => <tr key={stream.token}>
          <td title={stream.token}>{stream.token.slice(-12)}<br />{stream.render_mode === 'cpu' ? 'CPU' : stream.frame_policy === 'animation' ? 'GPU · 持续动画' : 'GPU · 交互优先'}</td>
          <td>{number(stream.target_fps)} / {stream.render_mode === 'cpu' ? '—' : number(stream.budget_fps)}</td>
          <td>{number(stream.render_fps)}</td><td>{number(stream.capture_fps)} / {number(stream.send_fps)} / {number(stream.display_fps)}</td><td>{number(stream.frame_bytes / 1024)} KB</td>
          <td>{ms(stream.snapshot_capture_encode_ms)} / {ms(stream.capture_delivery_ms)}</td><td>{ms(stream.publish_ms)} / {ms(stream.write_ms)}</td>
          <td>{ms(stream.decode_ms)} / {ms(stream.draw_ms)}</td><td>{ms(stream.frame_ack_ms)}</td><td>{Date.now() - Date.parse(stream.input_sample_at) < 10000 ? ms(stream.input_frame_ms) : '等待操作'}</td><td>{stream.server_dropped} / {stream.client_dropped}</td><td>{observation(stream)}</td>
        </tr>)}</tbody></table></div>
      {!data.streams.length && <p className="setting-description">打开一个观看链接后，可在这里查看实际画面链路数据。</p>}
      <p className="setting-description">截图时间包含截图与 JPEG 编码，仅在截图回退路径有样本；采集交付包含从浏览器帧时间戳到服务端收到事件的合并耗时。显示反馈包含发送、网络、解码、绘制和回传，不能等同于单向网络延迟。以上均为实测样本，不承诺指定帧率。</p>
      <p className="setting-description">页面回调 FPS 统计应用 requestAnimationFrame 的不同时间戳，不等于 GPU 物理呈现次数。输入耗时从观看端发送采样操作到 CDP 转发后交付的后续帧绘制；它包含排队和传输，但不能确认业务场景已响应或画面已物理呈现。暂无操作时等待新样本。</p>
    </> : <p className="setting-description">{data ? '当前节点暂未提供逐路链路测量。' : '正在读取各路性能…'}</p>}
  </section>
}
