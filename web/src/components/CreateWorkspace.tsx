import { changeSessionMode, normalizeRouteCounts } from '../session-limits'
import type { Dispatch, FormEvent, SetStateAction } from 'react'
import { ConsoleIcon as Icon } from './ConsoleIcon'
import { SessionQuotaField, PreloadCountField } from './SessionQuotaField'
import type { FormState, ResourcePolicy } from './ConsoleTypes'
import { ProjectPerformanceFields } from './ProjectPerformanceFields'
import { FieldLabel, HelpTooltip } from './HelpTooltip'

function StepHeading({ number, title, detail }: { number: string; title: string; detail: string }) {
  return <div className="config-step-heading"><span>{number}</span><h2>{title}</h2><HelpTooltip label={title}>{detail}</HelpTooltip></div>
}

export function CreateWorkspace({ values, setValues, resources, loading, onSubmit, onReset }: {
  values: FormState; setValues: Dispatch<SetStateAction<FormState>>; resources: ResourcePolicy | null
  loading: boolean; onSubmit: (event: FormEvent) => void; onReset: () => void
}) {
  const update = <K extends keyof FormState>(key: K, value: FormState[K]) => setValues(current => ({ ...current, [key]: value }))
  const capacity = resources?.channel_capacity
  const changeCounts = (patch: Partial<FormState>) => setValues(current => normalizeRouteCounts({ ...current, ...patch }))
  return <>
    <section className="console-metrics" aria-label="渲染资源策略">
      <article><span className="metric-icon"><Icon name="cpu" size={28} /></span><div><span className="metric-label">本软件 CPU 使用上限<HelpTooltip label="本软件 CPU 使用上限">所有渲染实例共享此配额，不限制其他程序的 CPU 占用。可在后台设置中调整。</HelpTooltip></span><div className="metric-value"><strong>{resources ? resources.cpu_max_percent ? `${resources.cpu_max_percent}%` : '未设置' : '—'}</strong></div></div></article>
      <article><span className="metric-icon"><Icon name="memory" size={28} /></span><div><span className="metric-label">内存保留策略<HelpTooltip label="内存保留策略">通过优先保留预加载路数，为重点项目保留已准备的实例。空闲实例持续占用内存及渲染资源。</HelpTooltip></span><strong className="metric-word">按需保留</strong></div></article>
      <article><span className="metric-icon"><Icon name="disk" size={28} /></span><div><span className="metric-label">资源缓存<HelpTooltip label="资源缓存">复用页面资源与浏览器缓存，减少重复加载。是否启用以服务器返回的状态为准。</HelpTooltip></span><strong className="metric-word">{resources ? resources.disk_cache_enabled ? '已启用' : '未启用' : '读取中'}</strong></div></article>
    </section>
    <section className="console-panel configuration-panel" aria-label="创建观看链接配置">
      <form onSubmit={onSubmit}>
        <fieldset disabled={loading}>
          <section className="config-step">
            <StepHeading number="01" title="项目与页面" detail="填写项目名称和原始 3D 页面地址" />
            <div className="two-fields"><div className="field"><FieldLabel htmlFor="name" title="项目名称" help="用于在已创建的链接中识别项目，可填写易于区分的名称，最多 100 个字符。">项目名称</FieldLabel><input id="name" value={values.name} onChange={e => update('name', e.target.value)} placeholder="例如：智慧园区实时大屏" maxLength={100} /></div><div className="field"><FieldLabel htmlFor="target-url" title="原始 3D 页面 URL" help="填写服务器能够访问的原始 3D 页面地址。页面在服务器浏览器中运行，观看者只接收画面并回传操作。">原始 3D 页面 URL <b>*</b></FieldLabel><input id="target-url" type="url" value={values.target_url} onChange={e => update('target_url', e.target.value)} placeholder="https://example.com/digital-twin" required /></div></div>
          </section>
          <section className="config-step">
            <StepHeading number="02" title="渲染配置" detail={values.render_mode === 'cpu' ? '选择渲染资源并设置画面参数。CPU 模式保持所选分辨率；实际帧率取决于场景复杂度、CPU 配额与服务器算力。' : '选择渲染资源并设置画面参数。GPU 模式使用服务器显卡渲染，由浏览器选择显卡；观看端只接收画面，实际性能取决于显卡及场景。'} />
            <div className="choice-grid" role="group" aria-label="渲染资源">{(['cpu', 'nvidia'] as const).map(mode => <button key={mode} type="button" className={`choice-card ${values.render_mode === mode ? 'is-selected' : ''}`} aria-pressed={values.render_mode === mode} onClick={() => update('render_mode', mode)}><Icon name={mode === 'cpu' ? 'cpu' : 'gpu'} size={36} /><span><strong>{mode === 'cpu' ? 'CPU 软件渲染' : '本机 GPU'}</strong><small>{mode === 'cpu' ? '无需显卡 · 默认' : '由浏览器选择显卡'}</small></span><i className="choice-check">{values.render_mode === mode && <Icon name="check" size={15} />}</i></button>)}</div>
            <div className="render-fields">
              <div className="field"><FieldLabel htmlFor="resolution" title="分辨率" help="固定分辨率按所选尺寸渲染。自适应按每位观看者的窗口比例渲染，窗口变化时同步调整；超大窗口会按比例限制渲染尺寸。分辨率越高，渲染及传输开销通常越大。">分辨率</FieldLabel><select id="resolution" value={values.resolution} onChange={e => setValues(current => ({ ...current, resolution: e.target.value, fps: e.target.value === '2560x1440' ? 30 : current.fps }))}><option value="adaptive">自适应观看窗口（推荐）</option><option value="1280x720">1280 × 720</option><option value="1920x1080">1920 × 1080</option><option value="2560x1440">2560 × 1440</option></select></div>
              <div className="field"><FieldLabel htmlFor="fps" title="目标帧率" help="设置期望的最高画面更新频率，实际帧率受服务器性能、场景和网络影响。2560 × 1440 限制为 30 FPS；90/120 FPS 仅在 JPEG 传输模式下可选。">目标帧率</FieldLabel><select id="fps" value={values.fps} onChange={e => update('fps', Number(e.target.value))}>{[30, 60, 90, 120].map(fps => <option key={fps} value={fps} disabled={fps > 30 && values.resolution === '2560x1440' || fps > 60 && resources?.transport !== 'jpeg'}>{fps} FPS</option>)}</select></div>
            </div>
          </section>
          <section className="config-step last-step">
            <StepHeading number="03" title="观看模式" detail="每位用户独占一个实时交互会话。单路同一时刻支持 1 位用户；多路为每位用户分配独立实例、画面和输入，操作互不干扰。所有项目共用服务器总通道配额。" />
            <div className="choice-grid" role="group" aria-label="观看模式">{(['single', 'multi'] as const).map(mode => <button key={mode} type="button" className={`choice-card ${values.concurrency_mode === mode ? 'is-selected' : ''}`} aria-pressed={values.concurrency_mode === mode} onClick={() => setValues(current => changeSessionMode(current, mode))}><Icon name={mode === 'single' ? 'user' : 'users'} size={34} /><span><strong>{mode === 'single' ? '单路会话' : '多路独立'}</strong><small>{mode === 'single' ? '1 位用户 · 1 个独立实例' : '每人独立交互，画面互不干扰'}</small></span><i className="choice-check">{values.concurrency_mode === mode && <Icon name="check" size={15} />}</i></button>)}</div>
            {values.concurrency_mode === 'multi' && <div className="create-session-limits">
              <SessionQuotaField compact id="max-viewers" value={values.max_viewers} capacity={capacity} onChange={value => changeCounts({ max_viewers: value })} />
              <PreloadCountField compact id="preload-count" value={values.preload_count} max={values.max_viewers} onChange={value => changeCounts({ preload_count: value })} />
            </div>}
            <ProjectPerformanceFields values={values} onChange={changeCounts} helpTooltips />
          </section>
        </fieldset>
        <div className="configuration-actions"><p><Icon name="info" size={18} />保存项目后，服务端开始预加载。</p><div><button className="quiet-button" type="button" disabled={loading} onClick={onReset}>重置</button><button className="primary-button" type="submit" disabled={loading}>{loading ? '正在创建…' : '预加载并生成链接'}<Icon name="arrow" size={18} /></button></div></div>
      </form>
    </section>
  </>
}
