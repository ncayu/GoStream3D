import { useEffect, useRef } from 'react'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, SVGRenderer])

export type GPUPoint = { id: string; name: string; utilization_percent: number | null }

export type MonitorPoint = {
  time: string
  rx_mbps: number
  tx_mbps: number
  cpu_percent?: number | null
  memory_percent?: number | null
  gpus?: GPUPoint[] | null
}
type Metric = 'network' | 'cpu' | 'memory' | 'gpu'
const labels = { network: '网络接收和发送速率', cpu: '整机 CPU 使用率', memory: '内存使用率', gpu: 'GPU 使用率' }
const time = (value: number) => new Date(value).toLocaleTimeString('zh-CN', { hour12: false })

export default function MonitorTrendChart({ history, theme, metric = 'network' }: { history: MonitorPoint[]; theme: string; metric?: Metric }) {
  const container = useRef<HTMLDivElement>(null)
  const chart = useRef<ReturnType<typeof echarts.init> | null>(null)
  const available = history.some(point => metric === 'gpu'
    ? point.gpus?.some(gpu => Number.isFinite(gpu.utilization_percent))
    : metric === 'network' || Number.isFinite(metric === 'cpu' ? point.cpu_percent : point.memory_percent))

  useEffect(() => {
    if (!container.current) return
    const instance = echarts.init(container.current, undefined, { renderer: 'svg' })
    chart.current = instance
    const observer = new ResizeObserver(() => instance.resize())
    observer.observe(container.current)
    return () => { observer.disconnect(); instance.dispose(); chart.current = null }
  }, [])

  useEffect(() => {
    if (!chart.current || !container.current) return
    const styles = getComputedStyle(container.current.closest('.admin-shell') || container.current)
    const color = (name: string, fallback: string) => styles.getPropertyValue(name).trim() || fallback
    const muted = color('--muted', '#90a9c1')
    const line = color('--line', '#29445f')
    const receive = color('--success', '#79dec6')
    const send = color('--accent', '#39bdf8')
    const percentage = metric !== 'network'
    const series = (name: string, tone: string, field: keyof Omit<MonitorPoint, 'time'>) => ({
      name, type: 'line' as const, smooth: false, connectNulls: false,
      showSymbol: history.length === 1, symbolSize: 7,
      lineStyle: { width: 2, color: tone }, itemStyle: { color: tone },
      areaStyle: percentage ? { color: tone, opacity: .08 } : undefined,
      data: history.map(point => [new Date(point.time).getTime(), Number.isFinite(point[field]) ? point[field] : null]),
    })
    const devices = new Map<string, string>()
    if (metric === 'gpu') {
      history.forEach(point => point.gpus?.forEach(gpu => devices.set(gpu.id, gpu.name)))
    }
    const tones = [send, receive, color('--warning', '#bf7515'), color('--danger', '#d94c60')]
    const gpuSeries = Array.from(devices, ([id, name], index) => {
      const data = history.map(point => {
        const value = point.gpus?.find(gpu => gpu.id === id)?.utilization_percent
        return [new Date(point.time).getTime(), Number.isFinite(value) ? value : null]
      })
      return {
        name: `GPU ${index + 1} · ${name}`, type: 'line' as const,
        smooth: false, connectNulls: false, showSymbol: data.filter(point => point[1] != null).length === 1, symbolSize: 7,
        lineStyle: { width: 2, color: tones[index % tones.length] },
        itemStyle: { color: tones[index % tones.length] },
        data,
      }
    })
    chart.current.setOption({
      animation: false,
      grid: { left: 58, right: 24, top: 16, bottom: metric === 'gpu' ? 58 : 32 },
      legend: metric === 'gpu' ? { type: 'scroll', bottom: 0, textStyle: { color: muted } } : undefined,
      tooltip: {
        trigger: 'axis', backgroundColor: color('--surface', '#102338'), borderColor: line,
        textStyle: { color: color('--text', '#edf7ff') },
        valueFormatter: (value: unknown) => value == null ? '暂无数据' : `${Number(value).toFixed(percentage ? 1 : 3)}${percentage ? '%' : ' Mbps'}`,
      },
      xAxis: {
        type: 'time', axisLine: { lineStyle: { color: line } }, axisTick: { show: false },
        axisLabel: { color: muted, hideOverlap: true, formatter: time },
        axisPointer: { label: { formatter: (params: { value: number }) => time(params.value) } },
      },
      yAxis: {
        type: 'value', min: 0, ...(percentage ? { max: 100, interval: 20 } : { minInterval: .1 }),
        name: percentage ? '%' : 'Mbps', nameTextStyle: { color: muted },
        axisLabel: { color: muted, formatter: percentage ? '{value}%' : '{value}' },
        splitLine: { lineStyle: { color: line, opacity: .6 } },
      },
      series: metric === 'gpu' ? gpuSeries : metric === 'network'
        ? [series('接收流量', receive, 'rx_mbps'), series('发送流量', send, 'tx_mbps')]
        : [series(labels[metric], metric === 'cpu' ? send : receive, metric === 'cpu' ? 'cpu_percent' : 'memory_percent')],
    }, true)
  }, [history, theme, metric])

  return <div className="monitor-trend-wrap">
    <div ref={container} className="monitor-echart" role="img" aria-label={`近一小时${labels[metric]}趋势图`} />
    {!available && <div className="monitor-trend-empty" role="status">暂无历史数据，等待下一次服务器采样</div>}
  </div>
}
