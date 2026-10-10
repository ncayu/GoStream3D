// Async Clipboard is preferred on secure origins. LAN HTTP uses the browser's
// selection copy command within the user's click, without changing permissions.
export async function copyText(text: string): Promise<void> {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch { /* Try selection copy if Async Clipboard was rejected. */ }
  }

  const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const selection = window.getSelection()
  const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange()) : []
  const field = document.createElement('textarea')
  field.value = text
  field.readOnly = true
  field.tabIndex = -1
  field.setAttribute('aria-hidden', 'true')
  field.style.cssText = 'position:fixed;left:-9999px;top:0;width:1px;height:1px;opacity:0;font-size:16px'
  document.body.appendChild(field)
  try {
    field.focus({ preventScroll: true })
    field.select()
    field.setSelectionRange(0, text.length)
    if (typeof document.execCommand !== 'function' || !document.execCommand('copy')) {
      throw new Error('Clipboard copy was not accepted by the browser')
    }
  } finally {
    field.remove()
    previousFocus?.focus({ preventScroll: true })
    if (selection) {
      selection.removeAllRanges()
      for (const range of ranges) selection.addRange(range)
    }
  }
}
