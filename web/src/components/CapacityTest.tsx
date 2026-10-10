import { useEffect, useState } from 'react'
import { fetchWithTimeout } from '../request'

type Project = { token: string; name: string; fps: number; resolution: string; render_mode: string }
type Report = { supported: boolean; status: string; current_streams?: number; recommended_streams?: number; cpu_max_percent?: number; error?: string; started_at?: string; project?: Project; request?: { min_fps: number; seconds: number; max_streams: number }; settings?: { gpu_fps_budget: number }; stages: { streams: number; passed: boolean; error?: string; measurements: { token: string; average_fps: number; p10_fps: number; render_fps: number; capture_fps: number; write_ms: number; bytes_per_second: number }[] }[] }
async function request<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetchWithTimeout(`/api/v1/admin/${path}`, { credentials: 'same-origin', cache: 'no-store', signal, ...(body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }) })
  const payload = await response.json() as { data?: T; msg?: string }
  if (!response.ok) throw new Error(payload.msg || '容量测试请求失败，请重试。')
  return payload.data as T
}
export function CapacityTest({ refreshVersion = 0 }: { refreshVersion?: number }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [selected, setSelected] = useState('')
  const [maxStreams, setMaxStreams] = useState(3)
  const [seconds, setSeconds] = useState(30)
  const [minFPS, setMinFPS] = useState(24)
  const [report, setReport] = useState<Report | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const controller = new AbortController(); let inFlight = false
    void request<Project[]>('sessions', undefined, controller.signal).then(value => { if (!controller.signal.aborted) setProjects(value) }).catch(cause => { if (!controller.signal.aborted) setError(String(cause)) })
    const poll = async () => { if (inFlight || document.visibilityState === 'hidden') return; inFlight = true; try { const value = await request<Report>('capacity', undefined, controller.signal); if (!controller.signal.aborted) setReport(value) } catch (cause) { if (!controller.signal.aborted) setError(String(cause)) } finally { inFlight = false } }
    void poll(); const timer = window.setInterval(() => void poll(), 3000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [refreshVersion])
  const running = report?.status === 'running'
  const start = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true); setError('')
    try { setReport(await request<Report>('capacity', { project_token: selected, max_streams: maxStreams, seconds, min_fps: minFPS })) }
    catch (cause) { setError(cause instanceof Error ? cause.message : '启动失败') } finally { setBusy(false) }
  }
  const cancel = async () => { setBusy(true); try { await request('capacity/cancel', {}) } catch (cause) { setError(String(cause)) } finally { setBusy(false) } }
  const project = projects.find(item => item.token === selected)
  const download = () => { if (!report) return; const url = URL.createObjectURL(new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' })); const a = document.createElement('a'); a.href = url; a.download = 'capacity-report.json'; a.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000) }
  return <section className="console-panel settings-resource-card" aria-labelledby="capacity-test-heading">
    <div className="resource-card-heading"><div><h2 id="capacity-test-heading">实际场景容量测试</h2><p>逐级测试独立项目副本，得到当前场景的服务器容量参考。</p></div></div>
    <p className="setting-description">测试会增加 CPU、GPU、显存和磁盘负载，建议在维护时运行。使用空闲通道，每级预热 10 秒；保留原项目配置。测试副本会加载原场景，GPU 按持续动画调度，结束或取消后自动清理。</p>
    <form onSubmit={event => void start(event)}><div className="field"><label htmlFor="capacity-project">测试项目</label><select id="capacity-project" value={selected} disabled={busy || running} required onChange={event => { setSelected(event.target.value); const item = projects.find(p => p.token === event.target.value); if (item) setMinFPS(Math.max(1, Math.floor(item.fps * .8))) }}><option value="">请选择实际场景</option>{projects.map(item => <option value={item.token} key={item.token}>{item.name || item.token} · {item.render_mode === 'cpu' ? 'CPU' : 'GPU'} · {item.resolution} @ {item.fps}</option>)}</select></div>
    <div className="capacity-fields"><div className="field"><label htmlFor="capacity-count">最多测试路数</label><input id="capacity-count" type="number" min={1} max={10} value={maxStreams} disabled={busy || running} required onChange={event => setMaxStreams(Number(event.target.value))} /></div><div className="field"><label htmlFor="capacity-seconds">每级采样秒数</label><input id="capacity-seconds" type="number" min={30} max={120} value={seconds} disabled={busy || running} required onChange={event => setSeconds(Number(event.target.value))} /></div><div className="field"><label htmlFor="capacity-fps">达标帧率 FPS</label><input id="capacity-fps" type="number" min={1} max={project?.fps || 120} step={1} value={minFPS} disabled={busy || running} required onChange={event => setMinFPS(Number(event.target.value))} /></div></div>
    <p className="setting-description">达标条件：每路至少 90% 的一秒窗口达到所填 FPS。自适应项目使用 1920×1080；静态或未持续更新的场景可能无法通过此测试。</p>
    <div className="resource-save-bar"><span>{running ? `正在测试 ${report.current_streams || 1} 路；取消后等待实例清理完成。` : '每次最多 10 路，不自动修改通道额度。'}</span>{running ? <button type="button" className="secondary-button" disabled={busy} onClick={() => void cancel()}>取消容量测试</button> : <button type="submit" className="primary-button" disabled={busy || !report?.supported || !selected}>开始场景容量测试</button>}</div></form>
    {report && !report.supported && <p className="setting-description">当前节点暂不支持本机场景容量测试。</p>}
    {report?.stages.length ? <div className="capacity-report"><p>项目：{report.project?.name || report.project?.token}；{report.project?.resolution} @ {report.project?.fps}；GPU 预算 {report.settings?.gpu_fps_budget}；达标 {report.request?.min_fps} FPS，每级 {report.request?.seconds} 秒。</p><p>已实测通过：{report.recommended_streams || 0} 路{running ? '（测试中）' : report.status === 'complete' ? '；仅为本次测试范围内的建议值。' : '；测试未完整结束，请查看错误并重新测试。'}</p><div className="monitor-table-wrap"><table className="monitor-table"><thead><tr><th>并发</th><th>结果</th><th>逐路平均 / P10 FPS</th></tr></thead><tbody>{report.stages.map(stage => <tr key={stage.streams}><td>{stage.streams} 路</td><td>{stage.error || (stage.passed ? '达标' : '未达标')}</td><td>{stage.measurements.map(m => `${m.average_fps.toFixed(1)} / ${m.p10_fps.toFixed(1)}`).join('；')}</td></tr>)}</tbody></table></div><button type="button" className="secondary-button" onClick={download}>下载测试报告</button></div> : null}
    <p className="setting-description">此测试在服务器本机接收并解码 JPEG，不包含远端浏览器绘制、实际网络及操作场景的影响，不能保证观看端流畅度。请结合逐路性能与目标设备实际观看验证，预留资源余量。</p>
    {(error || report?.error) && <div className="alert alert-error" role="alert">{error || report?.error}</div>}
  </section>
}
