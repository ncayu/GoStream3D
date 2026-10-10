import { useEffect, useMemo, useRef, useState } from 'react'
import { ConsoleIcon as Icon } from './ConsoleIcon'
import { streamURL, type Session } from './ConsoleTypes'
import { resolutionLabel } from '../viewport'
import type { BulkDeleteResult, DeleteProgress } from '../project-deletion'

function ProjectCheckbox({ checked, mixed = false, disabled = false, label, onChange }: {
  checked: boolean; mixed?: boolean; disabled?: boolean; label: string; onChange: (checked: boolean) => void
}) {
  const checkbox = useRef<HTMLInputElement>(null)
  useEffect(() => { if (checkbox.current) checkbox.current.indeterminate = mixed }, [mixed])
  return <input ref={checkbox} type="checkbox" className="project-checkbox" checked={checked} disabled={disabled} aria-label={label} onChange={event => onChange(event.target.checked)} />
}

function DeleteProjectsDialog({ items, busy, progress, error, onClose, onConfirm }: {
  items: Session[]; busy: boolean; progress: DeleteProgress; error: string; onClose: () => void; onConfirm: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const element = dialog.current
    const previous = document.activeElement as HTMLElement | null
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    element?.showModal()
    return () => {
      element?.close()
      document.body.style.overflow = overflow
      if (previous?.isConnected && !previous.matches(':disabled')) previous.focus()
      else document.querySelector<HTMLInputElement>('.project-filters input')?.focus()
    }
  }, [])
  const viewers = items.reduce((sum, item) => sum + item.viewers, 0)
  return <dialog ref={dialog} className="delete-project-dialog project-delete-modal console-panel" role="alertdialog" aria-labelledby="delete-project-title" aria-describedby="delete-project-description" onCancel={event => { event.preventDefault(); if (!busy) onClose() }}>
    <span className="delete-dialog-symbol"><Icon name="trash" size={28} /></span>
    <h2 id="delete-project-title">{items.length > 1 ? `删除所选 ${items.length} 个项目？` : '删除项目？'}</h2>
    <p id="delete-project-description">将删除以下项目的观看链接并回收渲染实例，此操作无法撤销。</p>
    <ul className="delete-project-list">{items.map(item => <li key={item.token}><strong>{item.name || '未命名项目'}</strong>{item.viewers > 0 && <small>{item.viewers} 人正在观看</small>}</li>)}</ul>
    {viewers > 0 && <p className="delete-viewer-warning">删除会断开这 {viewers} 位观看者的连接。</p>}
    {error && <div className="alert alert-error bulk-delete-errors" role="alert">{error}</div>}
    {busy && <p role="status">正在删除：{progress.completed} / {progress.total} 个项目</p>}
    <div><button type="button" autoFocus className="quiet-button" disabled={busy} onClick={onClose}>取消</button><button type="button" className="danger-button" disabled={busy} onClick={onConfirm}>{busy ? '删除中…' : error ? '重试未删除项目' : '确认删除'}</button></div>
  </dialog>
}

export function ProjectManager({ sessions, capacity, onCopy, onEdit, onStart, onStop, onWarm, onShowFPS, onDeleteMany, onCreate }: {
  sessions: Session[]; capacity?: number; onCopy: (item: Session, kind?: 'stream' | 'source') => Promise<void>; onEdit: (item: Session) => void
  onStart: (token: string) => Promise<void>; onStop: (token: string) => Promise<void>; onWarm: (token: string) => Promise<void>
  onShowFPS: (token: string, visible: boolean) => Promise<void>
  onCreate: () => void
  onDeleteMany: (tokens: string[], signal: AbortSignal, onProgress: (value: DeleteProgress) => void) => Promise<BulkDeleteResult>
}) {
  const [status, setStatus] = useState('all')
  const [search, setSearch] = useState('')
  const [render, setRender] = useState('all')
  const [mode, setMode] = useState('all')
  const [view, setView] = useState<'list' | 'grid'>('list')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState<Session[]>([])
  const [deleteBusy, setDeleteBusy] = useState(false)
  const [deleteProgress, setDeleteProgress] = useState<DeleteProgress>({ completed: 0, total: 0 })
  const [deleteError, setDeleteError] = useState('')
  const deletionRequest = useRef<AbortController | null>(null)
  useEffect(() => () => deletionRequest.current?.abort(), [])
  useEffect(() => { setSelected(new Set()) }, [status, render, mode, search])
  useEffect(() => {
    const closeMenus = (event: Event) => document.querySelectorAll<HTMLDetailsElement>('.project-more[open]').forEach(menu => {
      if (event.type !== 'click' || !menu.contains(event.target as Node)) menu.open = false
    })
    document.addEventListener('click', closeMenus)
    document.addEventListener('scroll', closeMenus, true)
    return () => { document.removeEventListener('click', closeMenus); document.removeEventListener('scroll', closeMenus, true) }
  }, [])
  const running = sessions.filter(item => item.running).length
  const stopped = sessions.filter(item => item.render_paused).length
  const cpu = sessions.filter(item => item.render_mode === 'cpu').length
  const filtered = useMemo(() => sessions.filter(item =>
    (status === 'all' || status === 'running' && item.running || status === 'stopped' && item.render_paused) &&
    (render === 'all' || item.render_mode === render) && (mode === 'all' || item.concurrency_mode === mode) &&
    `${item.name} ${item.target_url}`.toLowerCase().includes(search.trim().toLowerCase())
  ), [sessions, status, render, mode, search])
  useEffect(() => {
    const available = new Set(filtered.map(item => item.token))
    setSelected(current => {
      const next = new Set([...current].filter(token => available.has(token)))
      return next.size === current.size ? current : next
    })
  }, [filtered])
  useEffect(() => { setPage(1) }, [status, render, mode, search, pageSize])
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const items = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize)
  const selectedItems = filtered.filter(item => selected.has(item.token))
  const selectedOnPage = items.filter(item => selected.has(item.token)).length
  const allOnPage = items.length > 0 && selectedOnPage === items.length
  const toggleSelection = (tokens: string[], checked: boolean) => setSelected(current => {
    const next = new Set(current)
    tokens.forEach(token => checked ? next.add(token) : next.delete(token))
    return next
  })
  const openDelete = (projects: Session[]) => { setDeleteError(''); setDeleting(projects) }
  const confirmDelete = async () => {
    if (deletionRequest.current || !deleting.length) return
    const controller = new AbortController()
    deletionRequest.current = controller
    setDeleteBusy(true)
    setDeleteError('')
    const snapshot = deleting
    try {
      const result = await onDeleteMany(snapshot.map(item => item.token), controller.signal, setDeleteProgress)
      if (controller.signal.aborted) return
      const remaining = new Set([...result.failed.map(item => item.token), ...result.skipped])
      setSelected(current => new Set([...current].filter(token => !result.deleted.includes(token))))
      if (remaining.size) {
        setDeleting(snapshot.filter(item => remaining.has(item.token)))
        setDeleteError(`已删除 ${result.deleted.length} 个项目，${remaining.size} 个未删除。\n` + result.failed.map(failure => `${snapshot.find(item => item.token === failure.token)?.name || '未命名项目'}：${failure.message}`).join('\n'))
      } else setDeleting([])
    } catch (cause) {
      if (!controller.signal.aborted) setDeleteError(cause instanceof Error ? cause.message : '删除结果未确认，请刷新列表后检查。')
    } finally {
      if (deletionRequest.current === controller) { deletionRequest.current = null; setDeleteBusy(false) }
    }
  }
  const run = async (item: Session, action: (token: string) => Promise<void>) => {
    setPending(current => ({ ...current, [item.token]: true }))
    try { await action(item.token) } catch { /* The dashboard displays the API error; keep the dialog open. */ } finally { setPending(current => ({ ...current, [item.token]: false })) }
  }
  const statusText = (item: Session) => item.render_paused ? '已停止' : item.preload_suspended ? '空闲通道已让出' : item.preload_error ? '预加载失败' : item.running ? item.viewers > 0 ? '运行中' : '已预加载' : '预加载中'
  const statusCell = (item: Session) => <span className={`project-status ${item.render_paused ? 'is-stopped' : item.preload_error ? 'is-error' : item.running ? 'is-ready' : 'is-loading'}`} title={item.preload_error}><i />{statusText(item)}</span>
  const projectCell = (item: Session) => <div className="project-name"><span className="project-symbol"><Icon name="cube" size={27} /></span><div><strong title={item.name}>{item.name || '未命名项目'}</strong><div className="project-source-link"><small title={item.target_url}>{item.target_url}</small><button type="button" className="project-source-copy" onClick={() => void onCopy(item, 'source')} title="复制原始链接" aria-label={`复制 ${item.name || '项目'} 的原始链接`}><Icon name="copy" size={15} /></button></div></div></div>
  const configCell = (item: Session) => <div className="project-config"><span className={`mode-badge ${item.render_mode === 'nvidia' ? 'mode-gpu' : ''}`}>{item.render_mode === 'cpu' ? 'CPU' : 'GPU'}</span><small>{resolutionLabel(item.resolution)} · {item.fps} FPS</small></div>
  const modeCell = (item: Session) => <div className="project-mode"><strong>{item.concurrency_mode === 'single' ? '单路会话' : '多路独立'}</strong><small>{item.viewers}/{item.max_viewers} 路 · 独立交互</small><small title={item.pool_error}>{item.running || item.starting_instances ? `就绪 ${item.ready_instances ?? item.running_instances ?? 0} / 预加载 ${item.preload_count || 1} 路${item.starting_instances ? ' · 加载中' : ''}` : `预加载 ${item.preload_count || 1} 路`}{item.pool_error ? ' · 部分未就绪' : ''}</small></div>
  const linkCell = (item: Session) => <div className="project-link"><button type="button" className="project-copy" onClick={() => void onCopy(item)} title={streamURL(item)} aria-label={`复制 ${item.name || '项目'} 的观看链接`}><span>{streamURL(item).replace(/^https?:\/\//, '')}</span><Icon name="copy" size={17} /></button><a href={streamURL(item)} target="_blank" rel="noreferrer" title={item.running ? '打开观看页面' : '观看页面尚未就绪，请先启动渲染'} aria-label={`打开 ${item.name || '项目'} 的观看页面`}><Icon name="external" size={18} /></a></div>
  const actions = (item: Session) => <div className="project-actions">
    <button type="button" className={`quiet-button project-toggle ${item.running ? '' : 'start-render'}`} disabled={pending[item.token]} onClick={() => void run(item, item.running ? onStop : item.render_paused ? onStart : onWarm)}><Icon name={item.running ? 'pause' : 'play'} size={17} />{pending[item.token] ? '处理中…' : item.running ? '停止渲染' : item.render_paused ? '启动渲染' : '重新预加载'}</button>
    <button className="icon-button" type="button" disabled={item.viewers > 0 || pending[item.token]} onClick={() => onEdit(item)} aria-label={`编辑 ${item.name || '项目'}`} title={item.viewers > 0 ? '有用户正在观看，结束后可编辑' : '编辑链接配置'}><Icon name="edit" size={19} /></button>
    <details className="project-more" onToggle={event => {
      const menu = event.currentTarget
      if (!menu.open) return
      const anchor = menu.querySelector('summary')!.getBoundingClientRect()
      const dropdown = menu.querySelector<HTMLElement>('.dropdown-menu')!
      dropdown.style.left = Math.max(10, Math.min(window.innerWidth - 194, anchor.right - 184)) + 'px'
      dropdown.style.maxHeight = Math.max(80, window.innerHeight - 20) + 'px'
      dropdown.style.overflowY = 'auto'
      const height = Math.min(dropdown.scrollHeight, Math.max(80, window.innerHeight - 20))
      dropdown.style.top = (anchor.bottom + 7 + height > window.innerHeight - 10 ? Math.max(10, anchor.top - height - 7) : anchor.bottom + 7) + 'px'
      document.querySelectorAll<HTMLDetailsElement>('.project-more[open]').forEach(other => { if (other !== menu) other.open = false })
    }} onKeyDown={event => { if (event.key === 'Escape') event.currentTarget.open = false }}><summary className="icon-button" aria-label={`${item.name || '项目'} 的更多操作`}><Icon name="more" size={19} /></summary><div className="dropdown-menu"><a href={item.target_url} target="_blank" rel="noreferrer"><Icon name="external" size={16} />查看原始页面</a><button type="button" disabled={pending[item.token] || item.render_paused} onClick={e => { e.currentTarget.closest('details')?.removeAttribute('open'); void run(item, onWarm) }}><Icon name="refresh" size={16} />重新预加载</button><button type="button" disabled={pending[item.token]} onClick={e => { e.currentTarget.closest('details')?.removeAttribute('open'); void run(item, token => onShowFPS(token, item.show_fps === false)) }}><Icon name="monitor" size={16} />{item.show_fps === false ? '开启 FPS 显示' : '关闭 FPS 显示'}</button><button type="button" className="danger-action" disabled={pending[item.token]} onClick={e => { e.currentTarget.closest('details')?.removeAttribute('open'); openDelete([item]) }}><Icon name="trash" size={16} />删除项目</button></div></details>
  </div>
  return <>
    <section className="console-metrics" aria-label="项目运行概况">
      <article><span className="metric-icon"><Icon name="link" size={28} /></span><div><span>全部链接</span><strong>{sessions.length}</strong><small>CPU {cpu} · GPU {sessions.length - cpu}</small></div></article>
      <article><span className="metric-icon"><Icon name="server" size={28} /></span><div><span>运行实例</span><strong>{sessions.reduce((sum, item) => sum + item.running_instances, 0)}</strong><small>服务器总通道配置 {capacity ?? '—'} 路</small></div></article>
      <article><span className="metric-icon"><Icon name="users" size={28} /></span><div><span>当前观看人数</span><strong>{sessions.reduce((sum, item) => sum + item.viewers, 0)}</strong><small>每位用户拥有独立画面与操作</small></div></article>
    </section>
    <section className="console-panel project-management" aria-label="项目管理">
      <div className="project-panel-title"><h2>项目管理</h2><span className="soft-badge">{sessions.length} 个项目</span></div>
      <div className="project-toolbar"><div className="project-tabs" aria-label="项目状态筛选">{[['all', '全部', sessions.length], ['running', '已就绪', running], ['stopped', '已停止', stopped]].map(([id, name, count]) => <button key={id} type="button" className={status === id ? 'active' : ''} aria-pressed={status === id} onClick={() => setStatus(String(id))}>{name}<span>{count}</span></button>)}</div>
        <div className="project-filters"><label className="search-field"><Icon name="search" size={18} /><input type="search" placeholder="搜索项目名称或地址" value={search} onChange={e => setSearch(e.target.value)} aria-label="搜索项目名称或地址" /></label><label>渲染方式<select value={render} onChange={e => setRender(e.target.value)} aria-label="筛选渲染方式"><option value="all">全部</option><option value="cpu">CPU</option><option value="nvidia">GPU</option></select></label><label>观看模式<select value={mode} onChange={e => setMode(e.target.value)} aria-label="筛选观看模式"><option value="all">全部</option><option value="single">单路会话</option><option value="multi">多路独立</option></select></label><div className="view-switch" aria-label="项目显示方式"><button type="button" className={view === 'list' ? 'active' : ''} aria-pressed={view === 'list'} onClick={() => setView('list')} aria-label="列表视图"><Icon name="list" /></button><button type="button" className={view === 'grid' ? 'active' : ''} aria-pressed={view === 'grid'} onClick={() => setView('grid')} aria-label="卡片视图"><Icon name="grid" /></button></div></div>
      </div>
      <div className="project-bulk-bar"><div>{view === 'grid' && <label className="project-select-page"><ProjectCheckbox checked={allOnPage} mixed={selectedOnPage > 0 && !allOnPage} disabled={!items.length || deleteBusy} label="全选当前页项目" onChange={checked => toggleSelection(items.map(item => item.token), checked)} />全选当前页</label>}<span role="status">已选 {selectedItems.length} 个项目{selectedItems.length > selectedOnPage ? `（含其他页 ${selectedItems.length - selectedOnPage} 个）` : ''}</span>{selectedItems.length > 0 && <button type="button" className="quiet-button" disabled={deleteBusy} onClick={() => setSelected(new Set())}>取消选择</button>}</div><button type="button" className="danger-outline-button" disabled={!selectedItems.length || deleteBusy || selectedItems.some(item => pending[item.token])} onClick={() => openDelete(selectedItems)}><Icon name="trash" size={17} />批量删除{selectedItems.length ? `（${selectedItems.length}）` : ''}</button></div>
      {items.length === 0 ? <div className="empty-state"><span className="empty-symbol"><Icon name={sessions.length ? 'search' : 'link'} size={29} /></span><h3>{sessions.length ? '没有匹配的项目' : '开始创建第一个观看链接'}</h3><p>{sessions.length ? '调整关键词或筛选条件后重试。' : '添加原始 3D 页面，服务端预加载后即可分享。'}</p>{sessions.length ? <button className="quiet-button" type="button" onClick={() => { setSearch(''); setStatus('all'); setRender('all'); setMode('all') }}>清除筛选</button> : <button className="primary-button" type="button" onClick={onCreate}><Icon name="plus" size={18} />创建观看链接</button>}</div> : view === 'list' ? <div className="project-table-wrap"><table className="project-table"><thead><tr><th className="project-selection-cell"><ProjectCheckbox checked={allOnPage} mixed={selectedOnPage > 0 && !allOnPage} disabled={!items.length || deleteBusy} label="全选当前页项目" onChange={checked => toggleSelection(items.map(item => item.token), checked)} /></th><th>项目</th><th>渲染配置</th><th>观看模式</th><th>状态</th><th>观看链接</th><th>操作</th></tr></thead><tbody>{items.map(item => <tr key={item.token} className={selected.has(item.token) ? 'is-selected' : ''}><td className="project-selection-cell"><ProjectCheckbox checked={selected.has(item.token)} disabled={deleteBusy || pending[item.token]} label={`选择 ${item.name || '未命名项目'}`} onChange={checked => toggleSelection([item.token], checked)} /></td><td>{projectCell(item)}</td><td>{configCell(item)}</td><td>{modeCell(item)}</td><td>{statusCell(item)}</td><td>{linkCell(item)}</td><td>{actions(item)}</td></tr>)}</tbody></table></div> : <div className="project-card-grid">{items.map(item => <article className={'project-grid-card ' + (selected.has(item.token) ? 'is-selected' : '')} key={item.token}><div className="project-grid-heading"><ProjectCheckbox checked={selected.has(item.token)} disabled={deleteBusy || pending[item.token]} label={`选择 ${item.name || '未命名项目'}`} onChange={checked => toggleSelection([item.token], checked)} />{projectCell(item)}{statusCell(item)}</div><div className="project-grid-info">{configCell(item)}{modeCell(item)}</div>{item.preload_error && <p className="preload-error">{item.preload_error}</p>}{linkCell(item)}{actions(item)}</article>)}</div>}
      <div className="project-pagination"><div><span>共 {filtered.length} 个项目</span><label>每页<select value={pageSize} onChange={e => setPageSize(Number(e.target.value))} aria-label="每页项目数">{[10, 20, 50].map(size => <option key={size} value={size}>{size} 条</option>)}</select></label></div><div><button className="icon-button" aria-label="上一页" disabled={currentPage === 1} onClick={() => setPage(currentPage - 1)}><span>‹</span></button><span className="page-index">{currentPage}</span><span className="page-total">/ {totalPages}</span><button className="icon-button" aria-label="下一页" disabled={currentPage === totalPages} onClick={() => setPage(currentPage + 1)}><span>›</span></button></div></div>
    </section>
    <p className="console-note project-note"><Icon name="info" />1 路对应 1 位用户的独立渲染会话。多路会话的画面和鼠标操作互不干扰。</p>
    {deleting.length > 0 && <DeleteProjectsDialog items={deleting} busy={deleteBusy} progress={deleteProgress} error={deleteError} onClose={() => setDeleting([])} onConfirm={() => void confirmDelete()} />}
  </>
}
