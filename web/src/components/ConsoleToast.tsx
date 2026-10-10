import { useEffect } from 'react'
import { ConsoleIcon as Icon } from './ConsoleIcon'

export type CopyFeedback = { id: number; kind: 'success' | 'error' }

export function ConsoleToast({ feedback, onDismiss }: { feedback: CopyFeedback; onDismiss: () => void }) {
  useEffect(() => {
    const timer = window.setTimeout(onDismiss, feedback.kind === 'success' ? 3000 : 4500)
    return () => window.clearTimeout(timer)
  }, [feedback.id, feedback.kind, onDismiss])

  return <div className={`console-toast console-toast-${feedback.kind}`} role={feedback.kind === 'error' ? 'alert' : 'status'} aria-atomic="true">
    <span className="console-toast-symbol"><Icon name={feedback.kind === 'success' ? 'check' : 'close'} size={16} /></span>
    <strong>{feedback.kind === 'success' ? '复制成功' : '复制失败'}</strong>
    <button type="button" onClick={onDismiss} aria-label="关闭复制提示"><Icon name="close" size={16} /></button>
  </div>
}
