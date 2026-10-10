import { clampRoutes } from '../session-limits'
import { FieldLabel } from './HelpTooltip'

export function SessionQuotaField({ id, value, capacity, onChange, onOpenSettings, compact = false }: {
  id: string; value: number; capacity?: number; onChange: (value: number) => void; onOpenSettings?: () => void; compact?: boolean
}) {
  const limit = 100
  return <div className="channel-field">
    <div className="field"><FieldLabel htmlFor={id} title="最大独立会话数" help={compact && `可设 2–100 路，可以超过服务器总通道数（当前 ${capacity ?? '—'} 路）。每位用户获得独立实例；所有项目共用服务器通道，实际同时运行受总通道数限制。总通道数可在后台设置调整，未自动测算 GPU 性能上限。`}>最大独立会话数</FieldLabel><input id={id} type="number" min={2} max={limit} step={1} value={value} onChange={event => onChange(clampRoutes(Number(event.target.value), 2, limit))} required aria-describedby={`${id}-hint`} /></div>
    <div className="channel-quota-hint" id={`${id}-hint`}>
      {compact ? <p>服务器共 {capacity ?? '—'} 路 · <a href="#platform-settings" onClick={onOpenSettings}>后台设置</a></p> : <>
      <p>可设 2–100 路，可以超过服务器总通道数（当前 {capacity ?? '—'} 路）。项目最大会话数为独立配额，实际同时运行受所有项目共用的总通道数限制。</p>
      <p>服务器总通道数可在 <a href="#platform-settings" onClick={onOpenSettings}>后台设置</a> 中调整。总通道数由管理员配置，未自动测算 GPU 性能上限。</p>
      </>}
    </div>
  </div>
}

export function PreloadCountField({ id, value, max, onChange, compact = false }: {
  id: string; value: number; max: number; onChange: (value: number) => void; compact?: boolean
}) {
  return <div className="channel-field preload-count-field">
    <div className="field"><FieldLabel htmlFor={id} title="预加载路数" help={compact && `可设 1–${max} 路，不超过最大独立会话数。最多提前准备 ${value} 路，实际数量受可用通道限制；其余按需启动并等待各自的场景加载。预加载实例持续占用 CPU / GPU、内存和通道配额。`}>预加载路数</FieldLabel><input id={id} type="number" min={1} max={Math.max(1, max)} step={1} value={value} onChange={event => onChange(clampRoutes(Number(event.target.value), 1, Math.max(1, max)))} required aria-describedby={`${id}-hint`} /></div>
    <div className="channel-quota-hint" id={`${id}-hint`}>
      {compact ? <p>可选 1–{max} 路</p> : <>
      <p>可设 1–{max} 路，不超过最大独立会话数。最多提前准备 {value} 路，实际数量受可用通道限制；其余按需启动并等待各自的 3D 场景加载。</p>
      <p>预加载实例持续占用 CPU / GPU、内存和总通道配额，需要为其他项目预留通道。</p>
      </>}
    </div>
  </div>
}
