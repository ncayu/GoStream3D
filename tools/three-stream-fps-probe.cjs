// Short read-only stream benchmark. Opens three independent viewer routes and closes them.
const token = process.env.GOSTREAM3D_PROBE_TOKEN
if (!token) throw Error('Set GOSTREAM3D_PROBE_TOKEN to the selected project token')
const server = process.env.GOSTREAM3D_PROBE_BASE_URL || 'http://127.0.0.1:8888'
const base = server.replace(/^http/, 'ws') + '/api/v1/ws/' + encodeURIComponent(token)
const durationMs = Number(process.argv[2] || 12000)
const streamCount = Number(process.argv[3] || 3)
const tabSwitch = process.argv[4] === 'tab-switch'
const switchBack = process.argv[4] === 'switch-back' || tabSwitch
const allActive = process.argv[4] === 'all-active'
const oneVisible = process.argv[4] === 'one-visible'
const overrideActiveFps = Number(process.argv[5] || 0)
const sockets = []
const samples = Array.from({length: streamCount}, () => [])
const activeRoutes = Array.from({length: streamCount}, (_, index) => index)

async function open(index) {
  const socket = new WebSocket(base)
  socket.binaryType = 'arraybuffer'
  sockets[index] = socket
  socket.onmessage = event => {
    if (event.data instanceof ArrayBuffer) samples[index].push(performance.now())
  }
  await new Promise((resolve, reject) => {
    socket.onopen = resolve
    socket.onerror = reject
  })
}

function report(times, start, end) {
  const steady = times.filter(t => t >= start + 2000 && t <= end)
  const gaps = steady.slice(1).map((t, i) => t - steady[i]).sort((a, b) => a - b)
  return {
    frames: steady.length,
    fps: Number((steady.length / ((end - start - 2000) / 1000)).toFixed(1)),
    p50_gap_ms: gaps[Math.floor(gaps.length * .5)] && Math.round(gaps[Math.floor(gaps.length * .5)]),
    p95_gap_ms: gaps[Math.floor(gaps.length * .95)] && Math.round(gaps[Math.floor(gaps.length * .95)])
  }
}

async function setWorkerBudget(port, fps) {
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  const target = targets.find(item => item.type === 'page' && item.webSocketDebuggerUrl)
  if (!target) throw Error(`worker ${port} unavailable`)
  const socket = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  await new Promise((resolve, reject) => {
    socket.onmessage = event => {
      const message = JSON.parse(event.data)
      if (message.id !== 1) return
      message.error ? reject(Error(message.error.message)) : resolve()
    }
    socket.send(JSON.stringify({id: 1, method: 'Runtime.evaluate', params: {expression: `window.__g3dSetFrameRate(${fps})`}}))
  })
  socket.close()
}

;(async () => {
  try {
    for (let i = 0; i < streamCount; i++) await open(i)
    if (oneVisible) for (let i = 1; i < streamCount; i++) sockets[i].send(JSON.stringify({type: 'inactive'}))
    if (tabSwitch) for (let i = 0; i < streamCount - 1; i++) sockets[i].send(JSON.stringify({type: 'inactive'}))
    const start = performance.now()
    let activeIndex = switchBack ? 2 : 0
    const move = setInterval(() => {
      for (const index of allActive ? activeRoutes : [activeIndex]) {
        const socket = sockets[index]
        if (socket?.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({type: 'move', x: 700 + Math.round(Math.sin(performance.now() / 100) * 20), y: 500, button: 'left', buttons: 1}))
        }
      }
    }, 50)
    for (const index of allActive ? activeRoutes : [activeIndex]) sockets[index].send(JSON.stringify({type: 'down', x: 700, y: 500, button: 'left', buttons: 1}))
    let measureStart = start
    if (switchBack) {
      await new Promise(resolve => setTimeout(resolve, 5000))
      sockets[2].send(JSON.stringify({type: 'up', x: 700, y: 500, button: 'left', buttons: 0}))
      if (tabSwitch) sockets[2].send(JSON.stringify({type: 'inactive'}))
      activeIndex = 0
      sockets[0].send(JSON.stringify({type: 'activate'}))
      sockets[0].send(JSON.stringify({type: 'down', x: 700, y: 500, button: 'left', buttons: 1}))
      measureStart = performance.now()
      await new Promise(resolve => setTimeout(resolve, durationMs))
    } else {
      if (overrideActiveFps > 0) {
        await new Promise(resolve => setTimeout(resolve, 3000))
        await Promise.all([31000, 31001, 31002].map(port => setWorkerBudget(port, overrideActiveFps)))
        measureStart = performance.now()
      }
      await new Promise(resolve => setTimeout(resolve, durationMs))
    }
    clearInterval(move)
    for (const index of allActive ? activeRoutes : [activeIndex]) sockets[index].send(JSON.stringify({type: 'up', x: 700, y: 500, button: 'left', buttons: 0}))
    const end = performance.now()
    console.log(JSON.stringify({duration_ms: Math.round(end - start), switch_back: switchBack, tab_switch: tabSwitch, all_active: allActive, one_visible: oneVisible, override_active_fps: overrideActiveFps || undefined, streams: samples.slice(0, streamCount).map(s => report(s, measureStart, end))}))
  } finally {
    for (const socket of sockets) socket?.close()
  }
})().catch(error => { console.error(error); process.exitCode = 1 })
