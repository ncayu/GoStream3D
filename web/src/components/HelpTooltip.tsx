import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

export function HelpTooltip({ label, children }: { label: string; children: ReactNode }) {
  const id = useId()
  const trigger = useRef<HTMLButtonElement>(null)
  const popup = useRef<HTMLDivElement>(null)
  const closing = useRef<ReturnType<typeof setTimeout>>()
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState({ left: 0, top: 0 })
  const keepOpen = () => { clearTimeout(closing.current); setOpen(true) }
  const close = () => { clearTimeout(closing.current); setOpen(false) }
  const leave = () => {
    clearTimeout(closing.current)
    if (document.activeElement !== trigger.current) closing.current = setTimeout(() => setOpen(false), 150)
  }

  useLayoutEffect(() => {
    if (!open || !trigger.current || !popup.current) return
    const anchor = trigger.current.getBoundingClientRect()
    const box = popup.current.getBoundingClientRect()
    setPosition({
      left: Math.max(12, Math.min(anchor.left, innerWidth - box.width - 12)),
      top: anchor.bottom + box.height + 8 <= innerHeight - 12 ? anchor.bottom + 8 : Math.max(12, anchor.top - box.height - 8),
    })
  }, [open, children])

  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) => {
      if (!trigger.current?.contains(event.target as Node) && !popup.current?.contains(event.target as Node)) close()
    }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') close() }
    const scroll = (event: Event) => { if (!popup.current?.contains(event.target as Node)) close() }
    const otherHint = (event: PointerEvent) => {
      const target = (event.target as Element).closest?.('.help-trigger')
      if (target && target !== trigger.current) close()
    }
    document.addEventListener('pointerdown', outside, true)
    document.addEventListener('pointerover', otherHint)
    document.addEventListener('keydown', escape)
    document.addEventListener('scroll', scroll, true)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('pointerdown', outside, true)
      document.removeEventListener('pointerover', otherHint)
      document.removeEventListener('keydown', escape)
      document.removeEventListener('scroll', scroll, true)
      window.removeEventListener('resize', close)
    }
  }, [open])
  useEffect(() => () => clearTimeout(closing.current), [])

  return <>
    <button ref={trigger} type="button" className="help-trigger" aria-label={`${label}说明`} aria-describedby={open ? id : undefined}
      onPointerEnter={event => { if (event.pointerType !== 'touch') keepOpen() }} onPointerLeave={leave}
      onFocus={keepOpen} onBlur={close} onClick={keepOpen}><span aria-hidden="true">?</span></button>
    {open && createPortal(<div ref={popup} id={id} role="tooltip" className="console-help-tooltip" style={position}
      onPointerEnter={keepOpen} onPointerLeave={leave}><strong>{label}</strong><div>{children}</div></div>, document.body)}
  </>
}

export function FieldLabel({ htmlFor, children, help, title }: { htmlFor: string; children: ReactNode; help?: ReactNode; title: string }) {
  return <div className="field-label-row"><label htmlFor={htmlFor}>{children}</label>{help && <HelpTooltip label={title}>{help}</HelpTooltip>}</div>
}
