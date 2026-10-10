import { lazy, Suspense } from 'react'
import type { MonitorPoint } from './MonitorTrendChart'
const MonitorTrendChart = lazy(() => import('./MonitorTrendChart'))

export type GPU = { id: string; name: string; utilization_percent: number | null; encoder_percent: number | null; memory_used_bytes: number | null; memory_total_bytes: number | null }
const percentage = (value: number | null) => value === null ? '不可用' : `${value.toFixed(1)}%`
const memory = (value: number | null) => value === null ? '不可用' : `${(value / 1024 ** 3).toFixed(2)} GB`
export function GPUMonitor({ cards = [], message = '', history = [], theme = 'blue-tech', intervalSeconds = 30 }: { cards?: GPU[]; message?: string; history?: MonitorPoint[]; theme?: string; intervalSeconds?: number }) {
  return <section className="console-panel gpu-monitor" aria-labelledby="gpu-monitor-heading">
    <div className="project-panel-title"><h2 id="gpu-monitor-heading">GPU 与显存</h2><span className="soft-badge">{cards.length} 块 · 整机指标</span></div>
    {cards.length ? <div className="gpu-cards">{cards.map(card => <article className="gpu-card" key={card.id}>
      <h3>{card.name}</h3><dl className="monitor-info">
        <div><dt>GPU 使用率</dt><dd>{percentage(card.utilization_percent)}</dd></div>
        <div><dt>视频编码器</dt><dd>{percentage(card.encoder_percent)}</dd></div>
        <div><dt>显存占用</dt><dd>{memory(card.memory_used_bytes)} / {memory(card.memory_total_bytes)}</dd></div>
      </dl>
    </article>)}</div> : <p className="setting-description">{message || '当前未提供可读取的 GPU 数据。'}</p>}
    {cards.length > 0 && message && <p className="setting-description">{message}</p>}
    <div className="gpu-trend">
      <div className="monitor-panel-heading"><div><h3>GPU 使用率趋势</h3><small>近 1 小时 · 每 {intervalSeconds} 秒采样 · 含其他程序占用</small></div></div>
      <Suspense fallback={<div className="monitor-empty">正在加载图表…</div>}><MonitorTrendChart history={history} theme={theme} metric="gpu" /></Suspense>
    </div>
    <p className="setting-description">当前使用 NVIDIA 驱动的 nvidia-smi 采样，包含其他程序的占用。浏览器自行选择 GPU，不能据此把某块显卡的占用归属到单个项目；请结合逐路帧率和耗时查看。</p>
  </section>
}
