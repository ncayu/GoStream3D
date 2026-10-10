import { useEffect, useRef, useState } from 'react'
import { useViewerPresentation, ViewerFPS } from './ViewerPresentation'
import { watchViewport, type ViewportSize } from '../viewport'

type Props = { token: string; showFPS?: boolean }
type PointerInput = {
  type: 'move' | 'down' | 'up' | 'wheel'
  x: number
  y: number
  button: 'left' | 'middle' | 'right' | 'none'
  buttons: number
  clickCount?: number
  deltaX?: number
  deltaY?: number
}
type KeyInput = {
  type: 'keyDown' | 'keyUp'
  key: string
  code: string
  keyCode: number
  modifiers: number
  text: string
  repeat: boolean
  location: number
}
type VisibilityInput = { type: 'activate' | 'inactive' }

const buttonName = (button: number): PointerInput['button'] =>
  button === 0 ? 'left' : button === 1 ? 'middle' : button === 2 ? 'right' : 'none'

const wheelDelta = (value: number, mode: number, pageHeight: number) => {
  const pixels = value * (mode === WheelEvent.DOM_DELTA_LINE ? 16 : mode === WheelEvent.DOM_DELTA_PAGE ? pageHeight : 1)
  return Math.max(-32767, Math.min(32767, pixels * 2.5))
}

export function LocalCanvasPlayer({ token, showFPS: initialShowFPS = true }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const viewportRef = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('正在连接独立 3D 会话…')
  const [waitingSeconds, setWaitingSeconds] = useState(0)
  const [connected, setConnected] = useState(false)
  const [displayFPS, setDisplayFPS] = useState(0)
  const [limitExceeded, setLimitExceeded] = useState(false)
  const [adaptive, setAdaptive] = useState(false)
  const { showFPS } = useViewerPresentation(token, initialShowFPS)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const context = canvas.getContext('2d', { alpha: false })
    if (!context) { setError('浏览器不支持 Canvas 画面显示'); return }
    const abort = new AbortController()
    let stopped = false
    let socket: WebSocket | null = null
    let adaptiveMode = false
    let expectedSize: ViewportSize | null = null
    const viewport = watchViewport(viewportRef.current!, size => {
      if (!adaptiveMode || socket?.readyState !== WebSocket.OPEN) return
      if (expectedSize?.width === size.width && expectedSize.height === size.height) return
      pendingInput = null
      expectedSize = size
      socket.send(JSON.stringify(size))
    })
    let retryTimer: number | undefined
    let retryDelay = 1000
    let drawing = false
    let pendingFrame: { blob: Blob; id: number; inputID: number; source: WebSocket | null } | null = null
    let nextFrameID = 0
    let nextInputFrameID = 0
    let inputSequence = 0
    let lastInputSample = -Infinity
    const inputSamples = new Map<number, number>()
    let droppedFrames = 0
    let lastFeedbackAt = 0
    let measuredFPS = 0
    let mouseButton: PointerInput['button'] | null = null
    let mouseClickCount = 1
    let lastPointer = { x: 0, y: 0 }
    let lastClick = { at: 0, x: 0, y: 0, button: 'none' as PointerInput['button'], count: 0 }
    const touches = new Map<number, { x: number; y: number }>()
    let pinchDistance = 0
    let inputAnimation: number | undefined
    let pendingInput: PointerInput | null = null
    const pressedKeys = new Map<string, KeyInput>()
    let displayedFrames = 0
    let lastFPSAt = performance.now()
    let receivedFrame = false
    let blockedByLimit = false
    let waitingAt = performance.now()
    const setWaiting = (message: string) => { if (!stopped) setError(message) }
    const readStatus = async () => {
      const response = await fetch(`/api/v1/sessions/${encodeURIComponent(token)}`, { signal: abort.signal, cache: 'no-store' })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = await response.json()
      return payload.data as { render_paused?: boolean; running?: boolean; preload_error?: string; starting_instances?: number; available_instances?: number; viewer_limit_reached?: boolean; channel_capacity_full?: boolean }
    }
    const updateWaitingStatus = async () => {
      if (stopped || receivedFrame) return
      try {
        const status = await readStatus()
        if (stopped || receivedFrame) return
        if (socket?.readyState === WebSocket.OPEN) { setWaiting('已连接独立实例，正在接收首帧…'); return }
        blockedByLimit = !!(status.viewer_limit_reached || status.channel_capacity_full)
        setLimitExceeded(blockedByLimit)
        setWaiting(status.render_paused ? '渲染已由管理员停止，正在等待重新启动…' : blockedByLimit ? '路数超出限额，请查看配置' : status.preload_error && !status.running ? `服务端预加载失败，正在重试：${status.preload_error}` : status.starting_instances || !status.available_instances ? '服务端正在准备独立 3D 实例，模型加载完成后自动进入…' : '正在连接已预加载的独立实例，等待首帧…')
      } catch { /* The connection lifecycle reports transport failures. */ }
    }
    const waitingTimer = window.setInterval(() => {
      if (!receivedFrame && !blockedByLimit) { setWaitingSeconds(Math.floor((performance.now() - waitingAt) / 1000)); void updateWaitingStatus() }
    }, 2000)
    const fpsTimer = window.setInterval(() => {
      const now = performance.now()
      if (now - lastFPSAt < 1000) return
      measuredFPS = displayedFrames * 1000 / (now - lastFPSAt)
      setDisplayFPS(Number(measuredFPS.toFixed(1)))
      displayedFrames = 0
      lastFPSAt = now
    }, 2000)

    const drawLatest = async () => {
      if (drawing || !pendingFrame) return
      drawing = true
      while (!stopped && pendingFrame) {
        const frame = pendingFrame
        pendingFrame = null
        try {
          const decodeAt = performance.now()
          const image = await createImageBitmap(frame.blob)
          const decodeMS = performance.now() - decodeAt
          if (frame.source !== socket) { image.close(); continue }
          if (adaptiveMode && expectedSize && (image.width !== expectedSize.width || image.height !== expectedSize.height)) { image.close(); continue }
          if (!stopped) {
            if (canvas.width !== image.width || canvas.height !== image.height) {
              canvas.width = image.width
              canvas.height = image.height
            }
            const drawAt = performance.now()
            context.drawImage(image, 0, 0)
            const drawMS = performance.now() - drawAt
            if (!receivedFrame) { receivedFrame = true; setConnected(true); setError(''); setWaitingSeconds(0) }
            displayedFrames++
            const inputAt = inputSamples.get(frame.inputID)
            const inputMS = inputAt === undefined ? 0 : performance.now() - inputAt
            if (inputAt !== undefined) inputSamples.delete(frame.inputID)
            if (frame.id && (inputMS > 0 || performance.now() - lastFeedbackAt >= 500) && socket?.readyState === WebSocket.OPEN) {
              socket.send(JSON.stringify({ type: 'frameAck', frameID: frame.id, decodeMS, drawMS, displayFPS: measuredFPS, dropped: droppedFrames, inputMS }))
              lastFeedbackAt = performance.now()
            }
          }
          image.close()
        } catch (cause) {
          console.error('GoStream3D frame decode failed', cause)
        }
      }
      drawing = false
    }

    const connect = async () => {
      if (stopped) return
      receivedFrame = false; setConnected(false)
      if (!blockedByLimit) setWaiting('正在连接独立 3D 会话…')
      try {
        const response = await fetch(`/api/v1/sessions/${encodeURIComponent(token)}`, { signal: abort.signal })
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        const payload = await response.json() as { data: { resolution: string; render_paused?: boolean; running?: boolean; preload_error?: string; viewer_limit_reached?: boolean; channel_capacity_full?: boolean } }
        if (stopped) return
        adaptiveMode = payload.data.resolution === 'adaptive'
        setAdaptive(adaptiveMode)
        if (payload.data.render_paused) {
          setError('渲染已由管理员停止，正在等待重新启动…')
          retryTimer = window.setTimeout(() => void connect(), 10000)
          return
        }
        if (payload.data.viewer_limit_reached || payload.data.channel_capacity_full) {
          blockedByLimit = true
          setLimitExceeded(true)
          setWaitingSeconds(0)
          setWaiting('路数超出限额，请查看配置')
          retryTimer = window.setTimeout(() => void connect(), 5000)
          return
        }
		blockedByLimit = false
		setLimitExceeded(false)
		if (!payload.data.running) {
			setError(payload.data.preload_error ? `服务端预渲染失败，正在重试：${payload.data.preload_error}` : '服务端正在预加载 3D 场景，请稍候…')
		}
      } catch {
        if (stopped || abort.signal.aborted) return
        setError('无法检查渲染状态，正在重试…')
        retryTimer = window.setTimeout(() => void connect(), retryDelay)
        retryDelay = Math.min(retryDelay * 2, 10000)
        return
      }
      const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      socket = new WebSocket(`${scheme}//${window.location.host}/api/v1/ws/${encodeURIComponent(token)}`)
      socket.binaryType = 'blob'
      socket.onopen = () => {
        nextFrameID = 0
        nextInputFrameID = 0
        inputSamples.clear()
        lastFeedbackAt = 0
        droppedFrames = 0
        socket?.send(JSON.stringify({ type: 'telemetry' }))
        expectedSize = null
        viewport.sync()
        retryDelay = 1000
        setWaiting('已连接独立实例，正在接收首帧…')
        socket?.send(JSON.stringify({ type: document.visibilityState === 'visible' ? 'activate' : 'inactive' }))
      }
      socket.onmessage = (event) => {
        if (event.data instanceof Blob) {
          if (pendingFrame) droppedFrames++
          pendingFrame = { blob: event.data, id: nextFrameID, inputID: nextInputFrameID, source: socket }
          nextFrameID = 0
          nextInputFrameID = 0
          void drawLatest()
        } else if (typeof event.data === 'string') {
          try {
            const metadata = JSON.parse(event.data) as { type?: string; id?: number; inputID?: number }
            if (metadata.type === 'frame' && Number.isSafeInteger(metadata.id) && metadata.id! > 0) {
              nextFrameID = metadata.id!
              nextInputFrameID = Number.isSafeInteger(metadata.inputID) && metadata.inputID! > 0 ? metadata.inputID! : 0
            }
          } catch { /* Ignore unrecognized transport metadata. */ }
        }
      }
      socket.onerror = () => setError('画面连接失败，正在重试…')
      socket.onclose = () => {
        if (stopped) return
        pressedKeys.clear()
        mouseButton = null
        touches.clear()
        pinchDistance = 0
        canvas.style.cursor = 'grab'
        setConnected(false)
        receivedFrame = false; waitingAt = performance.now(); setWaitingSeconds(0)
        pendingInput = null
        pendingFrame = null
        setError('画面已断开，正在重连…')
        retryTimer = window.setTimeout(() => void connect(), retryDelay)
        retryDelay = Math.min(retryDelay * 2, 10000)
      }
    }
    fetch(`/api/v1/sessions/${encodeURIComponent(token)}`, { signal: abort.signal })
      .then((response) => { if (!response.ok) throw new Error(`HTTP ${response.status}`); return response.json() as Promise<{ data: { resolution: string } }> })
      .then((payload) => {
        const [width, height] = payload.data.resolution.split('x').map(Number)
        if (width > 0 && height > 0) { canvas.width = width; canvas.height = height }
        void connect()
      })
      .catch((cause) => { if (!abort.signal.aborted) setError(`无法打开画面：${String(cause)}`) })

    const transmit = (input: PointerInput | KeyInput | VisibilityInput | { type: 'text'; text: string }) => {
      if (socket?.readyState !== WebSocket.OPEN) return
      const now = performance.now()
      let inputID = 0
      if (input.type !== 'activate' && input.type !== 'inactive' && now - lastInputSample >= 500) {
        inputID = ++inputSequence
        lastInputSample = now
        inputSamples.set(inputID, now)
        for (const [id, at] of inputSamples) if (now - at > 30000 || inputSamples.size > 64) inputSamples.delete(id)
      }
      if ('x' in input && adaptiveMode) {
        if (input.type !== 'up' && expectedSize && (canvas.width !== expectedSize.width || canvas.height !== expectedSize.height)) return
        socket.send(JSON.stringify({ ...input, inputID, frameWidth: canvas.width, frameHeight: canvas.height }))
      } else socket.send(JSON.stringify({ ...input, inputID }))
    }
    const onVisible = () => {
      if (document.visibilityState !== 'visible') {
        onBlur()
        transmit({ type: 'inactive' })
        return
      }
      displayedFrames = 0
      lastFPSAt = performance.now()
      transmit({ type: 'activate' })
      viewport.sync()
    }
    const flushInput = () => {
      if (inputAnimation !== undefined) window.cancelAnimationFrame(inputAnimation)
      inputAnimation = undefined
      if (pendingInput) { transmit(pendingInput); pendingInput = null }
    }
    // High-rate mice can generate hundreds of moves per second. Keep the
    // latest position and send once per display frame. Wheel events retain
    // their count: some 3D controls zoom one step per event regardless of
    // its magnitude. Flush before button/key/wheel transitions to keep order.
    const send = (input: PointerInput | KeyInput | { type: 'text'; text: string }) => {
      if (socket?.readyState !== WebSocket.OPEN || !receivedFrame) return false
      if (input.type !== 'move') {
        flushInput()
        transmit(input)
        return true
      }
      if (pendingInput && pendingInput.buttons !== input.buttons) flushInput()
      pendingInput = input
      if (inputAnimation === undefined) inputAnimation = window.requestAnimationFrame(flushInput)
      return true
    }
    const coordinates = (event: MouseEvent | WheelEvent) => {
      const rect = canvas.getBoundingClientRect()
      return {
        x: Math.max(0, Math.min(canvas.width, (event.clientX - rect.left) * canvas.width / rect.width)),
        y: Math.max(0, Math.min(canvas.height, (event.clientY - rect.top) * canvas.height / rect.height)),
      }
    }
    const onDown = (event: MouseEvent) => {
      event.preventDefault()
      canvas.focus()
      canvas.style.cursor = 'grabbing'
      mouseButton = buttonName(event.button)
      const position = coordinates(event)
      lastPointer = position
      const now = performance.now()
      const sameClick = now - lastClick.at < 450 && Math.hypot(position.x - lastClick.x, position.y - lastClick.y) < 8 && lastClick.button === mouseButton
      mouseClickCount = sameClick ? Math.min(lastClick.count + 1, 3) : 1
      lastClick = { at: now, ...position, button: mouseButton, count: mouseClickCount }
      send({ type: 'down', ...position, button: mouseButton, buttons: event.buttons, clickCount: mouseClickCount })
    }
    const onMove = (event: MouseEvent) => {
      if (!mouseButton && event.target !== canvas) return
      lastPointer = coordinates(event)
      send({ type: 'move', ...lastPointer, button: 'none', buttons: mouseButton ? event.buttons || (mouseButton === 'left' ? 1 : mouseButton === 'right' ? 2 : 4) : event.buttons })
    }
    const onUp = (event: MouseEvent) => {
      if (!mouseButton) return
      lastPointer = coordinates(event)
      send({ type: 'up', ...lastPointer, button: mouseButton, buttons: event.buttons, clickCount: mouseClickCount })
      mouseButton = null
      canvas.style.cursor = 'grab'
    }
    const releaseKeys = () => {
      for (const input of [...pressedKeys.values()].reverse()) {
        send({ ...input, type: 'keyUp', modifiers: 0, text: '', repeat: false })
      }
      pressedKeys.clear()
    }
    const onBlur = () => {
      releaseKeys()
      if (mouseButton) send({ type: 'up', ...lastPointer, button: mouseButton, buttons: 0, clickCount: mouseClickCount })
      else if (touches.size === 1 && !pinchDistance) send({ type: 'up', ...[...touches.values()][0], button: 'left', buttons: 0 })
      mouseButton = null
      canvas.style.cursor = 'grab'
      touches.clear()
      pinchDistance = 0
    }
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      send({ type: 'wheel', ...coordinates(event), button: 'none', buttons: event.buttons,
        deltaX: wheelDelta(event.deltaX, event.deltaMode, canvas.height),
        deltaY: wheelDelta(event.deltaY, event.deltaMode, canvas.height) })
    }
    const onContext = (event: MouseEvent) => event.preventDefault()
    // Leave function keys, tab navigation, Escape and modified browser
    // shortcuts to the local browser. Shift is still useful for scene controls.
    const sceneKeys = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight',
      'Enter', 'Backspace', 'Delete', 'Home', 'End', 'PageUp', 'PageDown', 'Shift'])
    const onKey = (event: KeyboardEvent) => {
      if (event.isComposing || event.key === 'Process') return
      const id = event.code || `${event.key}:${event.location}`
      if (event.type === 'keyup') {
        const pressed = pressedKeys.get(id)
        if (!pressed) return
        event.preventDefault()
        send({ ...pressed, type: 'keyUp', text: '', repeat: false,
          modifiers: (event.shiftKey ? 8 : 0) })
        pressedKeys.delete(id)
        return
      }
      if (event.altKey || event.ctrlKey || event.metaKey ||
        !(event.key.length === 1 || sceneKeys.has(event.key))) {
        releaseKeys()
        return
      }
      if (event.repeat && !pressedKeys.has(id)) return
      const modifiers = (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0)
      const input: KeyInput = { type: 'keyDown', key: event.key, code: event.code,
        keyCode: event.keyCode, modifiers, text: event.key.length === 1 ? event.key : '',
        repeat: event.repeat, location: event.location }
      if (send(input)) { event.preventDefault(); pressedKeys.set(id, input) }
    }
    const onPaste = (event: ClipboardEvent) => {
      const pasted = event.clipboardData?.getData('text/plain')
      if (!pasted) return
      event.preventDefault()
      send({ type: 'text', text: pasted.slice(0, 4096) })
    }
    const onCompositionEnd = (event: CompositionEvent) => {
      if (event.data) send({ type: 'text', text: event.data.slice(0, 4096) })
    }
    const touchCoordinates = (touch: Touch) => coordinates({ clientX: touch.clientX, clientY: touch.clientY } as MouseEvent)
    const distance = () => {
      const [a, b] = [...touches.values()]
      return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0
    }
    const onTouchStart = (event: TouchEvent) => {
      event.preventDefault()
      canvas.focus()
      for (const touch of Array.from(event.changedTouches)) touches.set(touch.identifier, touchCoordinates(touch))
      if (touches.size === 1) send({ type: 'down', ...[...touches.values()][0], button: 'left', buttons: 1 })
      else if (touches.size === 2) {
        const first = [...touches.values()][0]
        send({ type: 'up', ...first, button: 'left', buttons: 0 })
        pinchDistance = distance()
      }
    }
    const onTouchMove = (event: TouchEvent) => {
      event.preventDefault()
      for (const touch of Array.from(event.changedTouches)) touches.set(touch.identifier, touchCoordinates(touch))
      if (touches.size === 1 && !pinchDistance) send({ type: 'move', ...[...touches.values()][0], button: 'none', buttons: 1 })
      else if (touches.size === 2) {
        const [a, b] = [...touches.values()]
        const next = distance()
        const delta = pinchDistance - next
        if (pinchDistance && Math.abs(delta) >= 2) send({ type: 'wheel', x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, button: 'none', buttons: 0, deltaX: 0, deltaY: delta * 4 })
        pinchDistance = next
      }
    }
    const onTouchEnd = (event: TouchEvent) => {
      event.preventDefault()
      const wasSingle = touches.size === 1 && !pinchDistance
      for (const touch of Array.from(event.changedTouches)) touches.delete(touch.identifier)
      if (wasSingle && event.changedTouches.length) send({ type: 'up', ...touchCoordinates(event.changedTouches[0]), button: 'left', buttons: 0 })
      if (touches.size === 0) pinchDistance = 0
    }
    canvas.addEventListener('mousedown', onDown)
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', onVisible)
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    window.addEventListener('blur', onBlur)
    window.addEventListener('pagehide', onBlur)
    canvas.addEventListener('blur', onBlur)
    canvas.addEventListener('wheel', onWheel, { passive: false })
    canvas.addEventListener('contextmenu', onContext)
    canvas.addEventListener('keydown', onKey)
    canvas.addEventListener('keyup', onKey)
    canvas.addEventListener('paste', onPaste)
    canvas.addEventListener('compositionend', onCompositionEnd)
    canvas.addEventListener('touchstart', onTouchStart, { passive: false })
    canvas.addEventListener('touchmove', onTouchMove, { passive: false })
    canvas.addEventListener('touchend', onTouchEnd, { passive: false })
    canvas.addEventListener('touchcancel', onTouchEnd, { passive: false })
    return () => {
      onBlur()
      stopped = true
      viewport.stop()
      abort.abort()
      if (retryTimer !== undefined) window.clearTimeout(retryTimer)
      window.clearInterval(fpsTimer)
      window.clearInterval(waitingTimer)
      if (inputAnimation !== undefined) window.cancelAnimationFrame(inputAnimation)
      pendingInput = null
      socket?.close()
      setConnected(false)
      canvas.removeEventListener('mousedown', onDown)
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('focus', onVisible)
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      window.removeEventListener('blur', onBlur)
      window.removeEventListener('pagehide', onBlur)
      canvas.removeEventListener('blur', onBlur)
      canvas.removeEventListener('wheel', onWheel)
      canvas.removeEventListener('contextmenu', onContext)
      canvas.removeEventListener('keydown', onKey)
      canvas.removeEventListener('keyup', onKey)
      canvas.removeEventListener('paste', onPaste)
      canvas.removeEventListener('compositionend', onCompositionEnd)
      canvas.removeEventListener('touchstart', onTouchStart)
      canvas.removeEventListener('touchmove', onTouchMove)
      canvas.removeEventListener('touchend', onTouchEnd)
      canvas.removeEventListener('touchcancel', onTouchEnd)
    }
  }, [token])

  return <>
    <div ref={viewportRef} className={`viewer-stage${adaptive ? ' viewer-adaptive' : ''}`}>
      <canvas ref={canvasRef} tabIndex={0} aria-label="可操作的远程画面，拖动旋转，滚轮缩放" className="viewer-canvas" />
      <ViewerFPS value={displayFPS} visible={showFPS} />
      {!connected && <div role="status" aria-live="polite" className="viewer-waiting"><div className="viewer-waiting-card"><strong>{limitExceeded ? '路数超出限额，请查看配置' : '正在准备独立 3D 会话'}</strong>{!limitExceeded && <span>{error || '正在等待首帧…'}</span>}{!limitExceeded && waitingSeconds > 0 && <small>已等待 {waitingSeconds} 秒 · 就绪后自动显示</small>}</div></div>}
    </div>
  </>
}
