import type { FormState } from './ConsoleTypes'
import { normalizeRouteCounts } from '../session-limits'
import { FieldLabel } from './HelpTooltip'

export function ProjectPerformanceFields({ values, onChange, helpTooltips = false }: {
  values: FormState; onChange: (patch: Partial<FormState>) => void; helpTooltips?: boolean
}) {
  const counts = normalizeRouteCounts(values)
  const policyHelp = '交互优先在无人操作时降低帧率；持续动画在观看期间保持动画，两者均受总帧率预算约束。'
  const preloadHelp = `可选 0–${counts.preload_count} 路，不超过预加载路数。通道不足时，其他项目的观看请求可回收超出此数量的空闲实例；正在观看的实例始终保留。`
  const fpsHelp = '控制观看画面右下角的帧率数据显示，保存后生效。此选项不改变目标帧率及后台性能采样。'
  return <div className={`two-fields project-performance-fields ${values.render_mode === 'nvidia' ? 'has-frame-policy' : ''}`}>
    {values.render_mode === 'nvidia' && <div className="field"><FieldLabel htmlFor="frame-policy" title="GPU 画面策略" help={helpTooltips && policyHelp}>GPU 画面策略</FieldLabel><select id="frame-policy" value={values.frame_policy} onChange={event => onChange({ frame_policy: event.target.value as FormState['frame_policy'] })}>
      <option value="interactive">交互优先</option><option value="animation">持续动画</option>
    </select>{!helpTooltips && <p className="field-hint">{policyHelp}</p>}</div>}
    <div className="field"><FieldLabel htmlFor="reserved-preloads" title="优先保留预加载路数" help={helpTooltips && preloadHelp}>优先保留预加载路数</FieldLabel><select id="reserved-preloads" value={counts.reserved_preloads} onChange={event => onChange({ reserved_preloads: Number(event.target.value) })} required>
      {Array.from({ length: counts.preload_count + 1 }, (_, value) => <option key={value} value={value}>{value} 路</option>)}
    </select>
      {!helpTooltips && <p className="field-hint">{preloadHelp}</p>}
    </div>
    <div className="field"><FieldLabel htmlFor="show-fps" title="观看页 FPS 显示" help={helpTooltips && fpsHelp}>观看页 FPS 显示</FieldLabel><select id="show-fps" value={values.show_fps ? 'on' : 'off'} onChange={event => onChange({ show_fps: event.target.value === 'on' })}>
      <option value="on">开启</option><option value="off">关闭</option>
    </select>{!helpTooltips && <p className="field-hint">{fpsHelp}</p>}</div>
  </div>
}
