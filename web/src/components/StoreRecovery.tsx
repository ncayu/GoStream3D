import { useEffect, useState } from 'react'
import { fetchWithTimeout } from '../request'

type Issue = { index: number; token: string; message: string }
type Backup = { id: string; created_at: string; valid_records: number; issues: number; readable: boolean }
type Store = { enabled: boolean; write_blocked: boolean; issues: Issue[]; archive: string; backups: Backup[] }
type Preview = { backup_id: string; digest: string; add: { token: string; name: string }[]; existing: number; issues: Issue[] }
async function storeRequest<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetchWithTimeout(`/api/v1/admin/store${path}`, { credentials: 'same-origin', cache: 'no-store', signal, ...(body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }) })
  const payload = await response.json() as { data?: T; msg?: string }
  if (!response.ok || !payload.data) throw new Error(payload.msg || '读取项目备份失败，请重试。')
  return payload.data
}
export function StoreHealthBanner({ refreshVersion = 0 }: { refreshVersion?: number }) {
  const [state, setState] = useState<Store | null>(null)
  useEffect(() => { const controller = new AbortController(); void storeRequest<Store>('', undefined, controller.signal).then(setState).catch(() => {}); return () => controller.abort() }, [refreshVersion])
  if (!state?.issues.length) return null
  return <div className="alert alert-error" role="alert">项目数据检测到 {state.issues.length} 项问题，已保留损坏原文并加载合法项目。{state.write_blocked && '原文件无法解析，已保护写入。'}请到<a href="#platform-settings">后台设置</a>查看备份与恢复。</div>
}
export function StoreRecovery({ refreshVersion = 0 }: { refreshVersion?: number }) {
  const [state, setState] = useState<Store | null>(null)
  const [selected, setSelected] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  useEffect(() => { const controller = new AbortController(); void storeRequest<Store>('', undefined, controller.signal).then(setState).catch(cause => { if (!controller.signal.aborted) setError(String(cause)) }); return () => controller.abort() }, [refreshVersion])
  const action = async (kind: 'backup' | 'preview' | 'restore') => {
    setBusy(true); setError(''); setNotice('')
    try {
      if (kind === 'preview') setPreview(await storeRequest<Preview>('/preview', { backup_id: selected }))
      else if (kind === 'backup') { await storeRequest('/backup', {}); setNotice('已创建项目配置备份。') }
      else if (preview) { const restored = await storeRequest<Preview>('/restore', { backup_id: preview.backup_id, digest: preview.digest }); setNotice(`已恢复 ${restored.add.length} 个缺失项目，观看地址保留。恢复的项目处于停止状态，可在链接列表启动。`); setPreview(null) }
      setState(await storeRequest<Store>(''))
    } catch (cause) { setError(cause instanceof Error ? cause.message : '操作失败') }
    finally { setBusy(false) }
  }
  return <section className="console-panel settings-resource-card" aria-labelledby="store-recovery-heading">
    <div className="resource-card-heading"><div><h2 id="store-recovery-heading">项目备份与恢复</h2><p>保留最近 20 份自动备份，含修改前与最新状态；手动备份和损坏原文独立保留。</p></div></div>
    {state?.issues.length ? <div className="alert alert-error" role="alert"><strong>{state.write_blocked ? '项目文件无法解析，写入已保护。' : '部分记录未加载，其余项目正常保留。'}</strong><ul>{state.issues.map((issue, index) => <li key={index}>{issue.index ? `第 ${issue.index} 条 ${issue.token}：` : ''}{issue.message}</li>)}</ul><p>损坏原文：{state.archive}</p></div> : <p className="setting-description">{state ? '当前项目数据未发现加载问题。' : '正在检查项目数据…'}</p>}
    <button type="button" className="secondary-button" disabled={busy || !state?.enabled || state.write_blocked} onClick={() => void action('backup')}>创建备份</button>
    <div className="field"><label htmlFor="store-backup">选择项目备份</label><select id="store-backup" value={selected} disabled={busy || !state?.enabled} onChange={event => { setSelected(event.target.value); setPreview(null) }}><option value="">请选择备份</option>{state?.backups.map(backup => <option key={backup.id} value={backup.id} disabled={!backup.readable}>{new Date(backup.created_at).toLocaleString('zh-CN')} · {backup.valid_records} 个项目{backup.issues ? ` · ${backup.issues} 条问题` : ''} · {backup.id.includes('-manual-') ? '手动' : backup.id.includes('-damaged-') ? '损坏原文' : '自动'}{!backup.readable ? ' · 无法解析' : ''}</option>)}</select></div>
    <p className="setting-description">恢复只补回缺失项目，保留现有项目、正在观看的实例及链接。恢复后停止渲染；浏览器缓存和其他平台设置不在此备份范围。</p>
    <button type="button" className="secondary-button" disabled={busy || !selected} onClick={() => void action('preview')}>预览可恢复项目</button>
    {preview && <div className="recovery-preview"><p>可恢复 {preview.add.length} 个项目；跳过 {preview.existing} 个已存在项目及 {preview.issues.length} 条不合法记录。</p><ul>{preview.add.map(item => <li key={item.token}>{item.name || item.token}</li>)}</ul><button type="button" className="primary-button" disabled={busy || !preview.add.length} onClick={() => void action('restore')}>恢复这些缺失项目</button></div>}
    {error && <div className="alert alert-error" role="alert">{error}</div>}{notice && <div className="alert alert-success" role="status">{notice}</div>}
  </section>
}
