import { useEffect, useRef, type RefObject } from 'react'
import { defaultTheme, themes } from '../themes'
import { type useAppearance, type ThemeSaveResult } from '../appearance'
import { ConsoleIcon as Icon } from './ConsoleIcon'

type Props = { appearance: ReturnType<typeof useAppearance> }

function SaveResultDialog({ result, onClose, returnFocus }: { result: ThemeSaveResult; onClose: () => void; returnFocus: RefObject<HTMLButtonElement> }) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const element = dialog.current
    element?.showModal()
    return () => { element?.close(); returnFocus.current?.focus() }
  }, [returnFocus])
  return <dialog ref={dialog} className="theme-result-dialog" role="alertdialog" aria-labelledby="theme-result-title" aria-describedby="theme-result-message" onCancel={event => { event.preventDefault(); onClose() }}>
    <div className={'theme-result-icon ' + (result.success ? 'is-success' : 'is-error')}><Icon name={result.success ? 'check' : 'info'} size={26} /></div>
    <h2 id="theme-result-title">{result.success ? '配色保存成功' : '配色保存失败'}</h2>
    <p id="theme-result-message">{result.message}{!result.success && ' 可再次点击“保存配色”重试。'}</p>
    <div className="edit-dialog-actions"><button type="button" className="primary-button" onClick={onClose} autoFocus>确定</button></div>
  </dialog>
}

export function ThemeSettings({ appearance }: Props) {
  const saveButton = useRef<HTMLButtonElement>(null)
  const { theme: selected, savedTheme, loading, error, saving, result, select: onSelect, save, retry, dismissResult } = appearance
  const disabled = loading || saving || savedTheme === null
  const current = themes.find(theme => theme.id === selected) || themes[0]
  const saved = themes.find(theme => theme.id === savedTheme)
  return <section className="theme-workspace" aria-label="外观设置">
    <section className="console-panel theme-gallery">
      <div className="theme-gallery-heading"><h2>界面配色</h2><button type="button" className="quiet-button" disabled={disabled} onClick={() => onSelect(defaultTheme)}><Icon name="refresh" size={16} />恢复默认配色</button></div>
      <p>选择配色可立即预览，点击“保存配色”后，下次打开自动使用已保存的配色。</p>
      {error && <div className="alert alert-error" role="alert">{error}<button type="button" className="quiet-button" onClick={retry}>重新加载配色</button></div>}
      <div className="console-theme-grid" aria-busy={loading}>{themes.map(theme => <button key={theme.id} type="button" disabled={disabled} className={'console-theme-card ' + (selected === theme.id ? 'is-selected' : '')} aria-pressed={selected === theme.id} onClick={() => onSelect(theme.id)}>
        <span className="theme-color-sample" style={{ background: theme.colors[2] }} />
        <span className="theme-color-name"><strong>{theme.name}</strong><small>{theme.description}</small></span>
        {selected === theme.id && <Icon name="check" size={18} />}
      </button>)}</div>
      <dl className="console-summary theme-preference-info"><div><dt>当前配色</dt><dd>{current.name}</dd></div><div><dt>已保存配色</dt><dd>{loading ? '正在加载…' : saved?.name || '未加载'}</dd></div><div><dt>界面模式</dt><dd>浅色内容区 · 深色导航</dd></div><div><dt>保存方式</dt><dd>服务端 SQLite 数据库</dd></div></dl>
      <div className="theme-save-bar"><p role="status">{loading ? '正在读取已保存的配色…' : savedTheme === null ? '请先加载已保存的配色。' : selected !== savedTheme ? '当前为预览配色，尚未保存。' : '当前配色与已保存的配色一致。'}</p><button ref={saveButton} type="button" className="primary-button" disabled={disabled} onClick={() => void save()}>{saving ? '正在保存…' : '保存配色'}</button></div>
      <p className="theme-scope-note"><Icon name="info" size={16} />配色仅用于管理页面，观看画面保持原样。</p>
    </section>
    {result && <SaveResultDialog result={result} onClose={dismissResult} returnFocus={saveButton} />}
  </section>
}
