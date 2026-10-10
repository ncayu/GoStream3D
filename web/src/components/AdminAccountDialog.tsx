import { useEffect, useRef, useState } from 'react'
import { fetchWithTimeout } from '../request'

export function AdminAccountDialog({ username, onClose, onSaved }: {
  username: string; onClose: () => void; onSaved: (username: string) => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [name, setName] = useState(username)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    dialog.current?.showModal()
    return () => {
      dialog.current?.close()
      document.querySelector<HTMLElement>('.account-menu summary')?.focus()
    }
  }, [])

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    if (saving) return
    setError('')
    if (newPassword !== confirmation) { setError('两次输入的新密码不一致。'); return }
    if (new TextEncoder().encode(newPassword).length > 72) { setError('新密码过长，请缩短后重试。'); return }
    setSaving(true)
    try {
      const response = await fetchWithTimeout('/api/v1/auth/account', {
        method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: name.trim(), current_password: currentPassword, new_password: newPassword }),
      })
      const body = await response.json().catch(() => null) as { msg?: string } | null
      if (!response.ok) throw new Error(body?.msg || '账号保存失败，请重试。')
      setCurrentPassword(''); setNewPassword(''); setConfirmation('')
      onSaved(name.trim())
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '账号保存失败，请重试。')
    } finally { setSaving(false) }
  }

  return <dialog ref={dialog} className="edit-dialog admin-account-dialog" aria-labelledby="admin-account-title"
    onCancel={event => { event.preventDefault(); if (!saving) onClose() }}>
    <h2 id="admin-account-title">修改账号密码</h2>
    <p className="admin-account-description">验证当前密码后修改管理员账号和密码。保存后需要重新登录。</p>
    <form onSubmit={event => void save(event)}>
      <div className="field"><label htmlFor="account-username">管理员账号</label><input id="account-username" value={name} onChange={event => setName(event.target.value)} autoComplete="username" maxLength={64} required disabled={saving} /></div>
      <div className="field"><label htmlFor="account-current-password">当前密码</label><input id="account-current-password" type="password" value={currentPassword} onChange={event => setCurrentPassword(event.target.value)} autoComplete="current-password" required disabled={saving} /></div>
      <div className="field"><label htmlFor="account-new-password">新密码</label><input id="account-new-password" type="password" value={newPassword} onChange={event => setNewPassword(event.target.value)} autoComplete="new-password" minLength={12} maxLength={72} required disabled={saving} /><small>至少 12 个字符，可保留原账号名称。</small></div>
      <div className="field"><label htmlFor="account-confirm-password">确认新密码</label><input id="account-confirm-password" type="password" value={confirmation} onChange={event => setConfirmation(event.target.value)} autoComplete="new-password" minLength={12} maxLength={72} required disabled={saving} /></div>
      {error && <div className="alert alert-error" role="alert">{error}</div>}
      <div className="edit-dialog-actions"><button type="button" className="quiet-button" onClick={onClose} disabled={saving}>取消</button><button type="submit" className="primary-button" disabled={saving}>{saving ? '保存中…' : '保存并重新登录'}</button></div>
    </form>
  </dialog>
}
