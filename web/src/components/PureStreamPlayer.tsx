import { useEffect, useRef, useState } from 'react'
import { useViewerPresentation, ViewerFPS } from './ViewerPresentation'
import { watchViewport, type ViewportSize } from '../viewport'
import { webrtcPerformance, type InboundSample, type PlaybackSample } from '../webrtc-performance'

type Props = { token: string; showFPS?: boolean }
type NekoMessage = {
  event: string
  id?: string
  sdp?: string
  lite?: boolean
  ice?: RTCIceServer[]
  data?: string
  heartbeat_interval?: number
  implicit_hosting?: boolean
  width?: number
  height?: number
  message?: string
}

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, Math.round(value)))

export function PureStreamPlayer({ token, showFPS: initialShowFPS = true }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const viewportRef = useRef<HTMLDivElement>(null)
  const [renderPaused, setRenderPaused] = useState(false)
  const [limitExceeded, setLimitExceeded] = useState(false)
  const [adaptive, setAdaptive] = useState(false)
  const [viewportError, setViewportError] = useState('')
  const [displayFPS, setDisplayFPS] = useState(0)
  const { showFPS } = useViewerPresentation(token, initialShowFPS)

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    let lastAt = performance.now()
    let lastFrames = 0
    const timer = window.setInterval(() => {
      const now = performance.now()
      const quality = video.getVideoPlaybackQuality?.()
      const frames = quality ? quality.totalVideoFrames - quality.droppedVideoFrames : 0
      setDisplayFPS(Math.max(0, Number(((frames - lastFrames) * 1000 / (now - lastAt)).toFixed(1))))
      lastFrames = frames
      lastAt = now
    }, 2000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    let disposed = false
    let ws: WebSocket | null = null
    let adaptiveMode = false
    let viewportBounds = { width: 3840, height: 2160 }
    let gpuViewport = false
    let expectedSize: ViewportSize | null = null
    const viewport = watchViewport(viewportRef.current!, size => {
      if (!adaptiveMode || ws?.readyState !== WebSocket.OPEN) return
      if (expectedSize?.width === size.width && expectedSize.height === size.height) return
      expectedSize = size
      setViewportError('')
      ws.send(JSON.stringify(size))
    }, () => viewportBounds, () => gpuViewport ? window.devicePixelRatio || 1 : 1)
    let pc: RTCPeerConnection | null = null
    let channel: RTCDataChannel | null = null
    let reconnectTimer: number | undefined
    let heartbeatTimer: number | undefined
    let retryDelay = 1000
    let peerID = ''
    let controllerID = ''
    let implicitHosting = true
    let pendingButton: number | null = null
    let pendingReleased = false
    let activeButton: number | null = null
    let inputFrame: number | undefined
    let pendingPointer: { clientX: number; clientY: number } | null = null
    let wheelBatches: { x: number; y: number }[] = []
    let screenWidth = 1920
    let screenHeight = 1080
    let pendingCandidates: RTCIceCandidateInit[] = []
    let messageChain = Promise.resolve()
    const abort = new AbortController()
    let previousInbound: InboundSample | undefined
    let previousPlayback: PlaybackSample | undefined
    let sampling = false

    const sendJSON = (value: object) => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(value))
    }

    // One bounded getStats call every three seconds. Only the connected video's
    // counters are reported; no audio traffic or synthetic rendering is added.
    const performanceTimer = window.setInterval(() => {
      if (sampling || document.visibilityState === 'hidden' || pc?.connectionState !== 'connected' || ws?.readyState !== WebSocket.OPEN || !video.videoWidth) return
      const current = pc
      sampling = true
      void current.getStats().then(report => {
        if (disposed || pc !== current) return
        let inbound: RTCInboundRtpStreamStats | undefined
        report.forEach(r => { if (r.type === 'inbound-rtp' && (r.kind === 'video' || r.mediaType === 'video')) inbound = r })
        if (!inbound) { previousInbound = undefined; previousPlayback = undefined; return }
        const quality = video.getVideoPlaybackQuality?.()
        const playback = { at: performance.now(), total: quality?.totalVideoFrames ?? NaN, dropped: quality?.droppedVideoFrames ?? NaN, width: video.videoWidth, height: video.videoHeight }
        const transport = inbound.transportId ? report.get(inbound.transportId) : undefined
        const pair = transport?.selectedCandidatePairId ? report.get(transport.selectedCandidatePairId) : undefined
        const codec = inbound.codecId ? report.get(inbound.codecId)?.mimeType || '' : ''
        const metrics = webrtcPerformance(previousInbound, inbound, previousPlayback, playback, codec, pair?.currentRoundTripTime)
        previousInbound = inbound; previousPlayback = playback
        if (metrics) sendJSON({ type: 'webrtc-performance', metrics })
      }).catch(() => { previousInbound = undefined; previousPlayback = undefined })
        .finally(() => { sampling = false })
    }, 3000)

    const sendPointer = (event: { clientX: number; clientY: number }) => {
      if (channel?.readyState !== 'open') return
      if (adaptiveMode && expectedSize && (video.videoWidth !== expectedSize.width || video.videoHeight !== expectedSize.height)) return
      const rect = video.getBoundingClientRect()
      if (rect.width <= 0 || rect.height <= 0) return
      const width = video.videoWidth || screenWidth
      const height = video.videoHeight || screenHeight
      const x = clamp((event.clientX - rect.left) * width / rect.width, 0, width - 1)
      const y = clamp((event.clientY - rect.top) * height / rect.height, 0, height - 1)
      const buffer = new ArrayBuffer(7)
      const data = new DataView(buffer)
      data.setUint8(0, 0x01)
      data.setUint16(1, 4, true)
      data.setUint16(3, x, true)
      data.setUint16(5, y, true)
      channel.send(buffer)
    }

    const sendButton = (pressed: boolean, button: number) => {
      if (channel?.readyState !== 'open') return
      const buffer = new ArrayBuffer(11)
      const data = new DataView(buffer)
      data.setUint8(0, pressed ? 0x03 : 0x04)
      data.setUint16(1, 8, true)
      data.setBigUint64(3, BigInt(button), true)
      channel.send(buffer)
    }

    const scheduleInput = () => {
      if (inputFrame === undefined) inputFrame = window.requestAnimationFrame(flushInput)
    }
    const flushInput = () => {
      if (inputFrame !== undefined) window.cancelAnimationFrame(inputFrame)
      inputFrame = undefined
      if (channel?.readyState !== 'open') return
      // Keep the latest move locally when SCTP is congested instead of sending
      // a trail of obsolete positions. Button transitions remain reliable.
      if (channel.bufferedAmount > 1024) { scheduleInput(); return }
      if (pendingPointer) { sendPointer(pendingPointer); pendingPointer = null }
      if (controllerID !== peerID || !peerID) return
      while (wheelBatches.length) {
        const batch = wheelBatches[0]
        const x = Math.trunc(Math.max(-16, Math.min(16, batch.x)))
        const y = Math.trunc(Math.max(-16, Math.min(16, batch.y)))
        if (x === 0 && y === 0) {
          if (wheelBatches.length > 1) { wheelBatches.shift(); continue }
          break
        }
        const buffer = new ArrayBuffer(7)
        const data = new DataView(buffer)
        data.setUint8(0, 0x02)
        data.setUint16(1, 4, true)
        // Neko's legacy protocol uses X11 button steps, opposite to DOM deltas.
        data.setInt16(3, -x, true)
        data.setInt16(5, -y, true)
        channel.send(buffer)
        batch.x -= x; batch.y -= y
        if (Math.abs(batch.x) >= 1 || Math.abs(batch.y) >= 1) { scheduleInput(); break }
        if (wheelBatches.length > 1) wheelBatches.shift()
        else break
      }
    }

    const onMouseDown = (event: MouseEvent) => {
      event.preventDefault()
      if (adaptiveMode && expectedSize && (video.videoWidth !== expectedSize.width || video.videoHeight !== expectedSize.height)) return
      video.focus()
      flushInput()
      pendingPointer = null
      sendPointer(event)
      const button = event.button + 1
      if (controllerID === peerID && peerID) {
        sendButton(true, button)
        activeButton = button
      } else if (implicitHosting) {
        pendingButton = button
        pendingReleased = false
        sendJSON({ event: 'control/request' })
      }
    }
    const onMouseMove = (event: MouseEvent) => {
      pendingPointer = { clientX: event.clientX, clientY: event.clientY }
      scheduleInput()
    }
    const onMouseUp = (event: MouseEvent) => {
      event.preventDefault()
      flushInput()
      pendingPointer = null
      sendPointer(event)
      if (activeButton !== null) {
        sendButton(false, activeButton)
        activeButton = null
      } else if (pendingButton !== null) {
        pendingReleased = true
      }
    }
    const onMouseLeave = () => {
      if (activeButton !== null) sendButton(false, activeButton)
      activeButton = null
    }
    const onContextMenu = (event: MouseEvent) => event.preventDefault()
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      if (adaptiveMode && expectedSize && (video.videoWidth !== expectedSize.width || video.videoHeight !== expectedSize.height)) return
      if (channel?.readyState !== 'open') return
      const scale = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 1 / 3 : event.deltaMode === WheelEvent.DOM_DELTA_PAGE ? 3 : 1 / 100
      const x = event.deltaX * scale, y = event.deltaY * scale
      if (!Number.isFinite(x) || !Number.isFinite(y)) return
      pendingPointer = { clientX: event.clientX, clientY: event.clientY }
      let last = wheelBatches.at(-1)
      if (last && (last.x * x < 0 || last.y * y < 0)) {
        // Keep complete operations in order; discard only sub-step residue on
        // direction reversal so the next gesture does not feel unresponsive.
        if (Math.abs(last.x) < 1 && Math.abs(last.y) < 1) wheelBatches.pop()
        last = undefined
      }
      if (last) { last.x += x; last.y += y }
      else wheelBatches.push({ x, y })
      if (controllerID !== peerID && implicitHosting) sendJSON({ event: 'control/request' })
      scheduleInput()
    }
    video.addEventListener('mousedown', onMouseDown)
    video.addEventListener('mousemove', onMouseMove)
    video.addEventListener('mouseup', onMouseUp)
    video.addEventListener('mouseleave', onMouseLeave)
    video.addEventListener('contextmenu', onContextMenu)
    video.addEventListener('wheel', onWheel, { passive: false })
    window.addEventListener('blur', onMouseLeave)

    const startHeartbeat = (seconds: number) => {
      if (heartbeatTimer !== undefined) window.clearInterval(heartbeatTimer)
      heartbeatTimer = window.setInterval(() => sendJSON({ event: 'client/heartbeat' }), Math.max(5, seconds) * 1000)
    }

    const cleanupConnection = () => {
      previousInbound = undefined
      previousPlayback = undefined
      if (inputFrame !== undefined) window.cancelAnimationFrame(inputFrame)
      inputFrame = undefined
      pendingPointer = null
      wheelBatches = []
      if (heartbeatTimer !== undefined) window.clearInterval(heartbeatTimer)
      heartbeatTimer = undefined
      if (ws) {
        ws.onclose = null
        ws.onmessage = null
        ws.close()
        ws = null
      }
      channel?.close()
      channel = null
      pc?.close()
      pc = null
      video.srcObject = null
      peerID = ''
      controllerID = ''
      activeButton = null
      pendingButton = null
      pendingCandidates = []
    }

    const scheduleReconnect = () => {
      if (disposed) return
      cleanupConnection()
      reconnectTimer = window.setTimeout(() => void connect(), retryDelay)
      retryDelay = Math.min(retryDelay * 2, 10000)
    }

    const createPeer = (lite: boolean, ice: RTCIceServer[]) => {
      pc?.close()
      pc = new RTCPeerConnection(lite ? {} : { iceServers: ice })
      const current = pc
      current.ontrack = (event) => {
        if (event.track.kind !== 'video') return
        const receiver = event.receiver as { jitterBufferTarget?: number; playoutDelayHint?: number }
        try {
          if ('jitterBufferTarget' in receiver) receiver.jitterBufferTarget = 0
          else if ('playoutDelayHint' in receiver) receiver.playoutDelayHint = 0
        } catch { /* Older browsers may expose a read-only latency hint. */ }
        // This player is muted. A video-only stream avoids waiting for unused
        // audio to synchronize its larger playback buffer with video.
        const stream = new MediaStream([event.track])
        if (video.srcObject !== stream) video.srcObject = stream
        void video.play().catch(() => {})
      }
      current.onicecandidate = (event) => {
        if (event.candidate) sendJSON({ event: 'signal/candidate', data: JSON.stringify(event.candidate.toJSON()) })
      }
      current.onconnectionstatechange = () => {
        if (current.connectionState === 'failed') ws?.close()
      }
      current.onnegotiationneeded = async () => {
        if (current.signalingState !== 'stable') return
        try {
          const offer = await current.createOffer()
          await current.setLocalDescription(offer)
          sendJSON({ event: 'signal/offer', sdp: offer.sdp })
        } catch (error) {
          console.error('Neko renegotiation failed', error)
        }
      }
      channel = current.createDataChannel('data')
    }

    const answerOffer = async (sdp: string) => {
      if (!pc) return
      await pc.setRemoteDescription({ type: 'offer', sdp })
      for (const transceiver of pc.getTransceivers()) {
        if (transceiver.receiver.track.kind === 'audio') transceiver.direction = 'inactive'
      }
      for (const candidate of pendingCandidates) await pc.addIceCandidate(candidate)
      pendingCandidates = []
      const answer = await pc.createAnswer()
      await pc.setLocalDescription(answer)
      sendJSON({ event: 'signal/answer', sdp: answer.sdp, displayname: 'GoStream3D' })
    }

    const handleMessage = async (raw: string) => {
      const msg = JSON.parse(raw) as NekoMessage
      switch (msg.event) {
        case 'system/init':
          implicitHosting = msg.implicit_hosting ?? true
          startHeartbeat(msg.heartbeat_interval || 20)
          break
        case 'signal/provide':
          peerID = msg.id || ''
          createPeer(!!msg.lite, msg.ice || [])
          if (msg.sdp) await answerOffer(msg.sdp)
          retryDelay = 1000
          break
        case 'signal/offer':
          if (msg.sdp) await answerOffer(msg.sdp)
          break
        case 'signal/answer':
          if (pc && msg.sdp) await pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp })
          break
        case 'signal/candidate':
          if (msg.data) {
            const candidate = JSON.parse(msg.data) as RTCIceCandidateInit
            if (pc?.remoteDescription) await pc.addIceCandidate(candidate)
            else pendingCandidates.push(candidate)
          }
          break
        case 'control/locked':
          controllerID = msg.id || ''
          if (controllerID === peerID) scheduleInput()
          if (controllerID === peerID && pendingButton !== null) {
            sendButton(true, pendingButton)
            if (pendingReleased) sendButton(false, pendingButton)
            else activeButton = pendingButton
            pendingButton = null
            pendingReleased = false
          }
          break
        case 'control/release':
          controllerID = ''
          activeButton = null
          break
        case 'screen/resolution':
          if (msg.width && msg.height) {
            screenWidth = msg.width
            screenHeight = msg.height
          }
          break
        case 'viewport/error':
          setViewportError(msg.message || '自适应尺寸调整失败')
          break
      }
    }

    const connect = async () => {
      if (disposed) return
      try {
        const response = await fetch(`/api/v1/sessions/${encodeURIComponent(token)}`, { signal: abort.signal })
        if (!response.ok) throw new Error(`session lookup failed: ${response.status}`)
        const payload = await response.json() as { data: { resolution: string; render_mode?: string; max_width?: number; max_height?: number; render_paused?: boolean; viewer_limit_reached?: boolean; channel_capacity_full?: boolean } }
        if (payload.data.render_paused) {
          setRenderPaused(true)
          scheduleReconnect()
          return
        }
        setRenderPaused(false)
        if (payload.data.viewer_limit_reached || payload.data.channel_capacity_full) {
          setLimitExceeded(true)
          scheduleReconnect()
          return
        }
        setLimitExceeded(false)
        adaptiveMode = payload.data.resolution === 'adaptive'
        gpuViewport = payload.data.render_mode === 'nvidia'
        viewportBounds = { width: payload.data.max_width ?? 3840, height: payload.data.max_height ?? 2160 }
        setAdaptive(adaptiveMode)
        const [width, height] = payload.data.resolution.split('x').map(Number)
        if (width > 0 && height > 0) { screenWidth = width; screenHeight = height }
        if (disposed) return
        const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
        ws = new WebSocket(`${scheme}//${window.location.host}/api/v1/ws/${encodeURIComponent(token)}`)
        ws.onopen = () => { startHeartbeat(20); expectedSize = null; viewport.sync() }
        ws.onmessage = (event) => {
          messageChain = messageChain.then(() => handleMessage(String(event.data))).catch((error) => {
            console.error('Neko signaling failed', error)
            ws?.close()
          })
        }
        ws.onclose = scheduleReconnect
      } catch (error) {
        if (!disposed && !abort.signal.aborted) {
          console.error('GoStream3D connection failed', error)
          scheduleReconnect()
        }
      }
    }
    void connect()

    return () => {
      disposed = true
      window.clearInterval(performanceTimer)
      viewport.stop()
      abort.abort()
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer)
      cleanupConnection()
      video.removeEventListener('mousedown', onMouseDown)
      video.removeEventListener('mousemove', onMouseMove)
      video.removeEventListener('mouseup', onMouseUp)
      video.removeEventListener('mouseleave', onMouseLeave)
      video.removeEventListener('contextmenu', onContextMenu)
      video.removeEventListener('wheel', onWheel)
      window.removeEventListener('blur', onMouseLeave)
    }
  }, [token])

  return <div ref={viewportRef} className={`viewer-stage${adaptive ? ' viewer-adaptive' : ''}`}>
    <video ref={videoRef} autoPlay muted playsInline tabIndex={0} className="viewer-video" />
    <ViewerFPS value={displayFPS} visible={showFPS} />
    {renderPaused && <div role="status" className="viewer-paused">渲染已由管理员停止，正在等待重新启动…</div>}
    {!renderPaused && limitExceeded && <div role="status" className="viewer-paused">路数超出限额，请查看配置</div>}
    {viewportError && <div role="alert" className="viewer-viewport-error">{viewportError}</div>}
  </div>
}
