import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

const source = await readFile(new URL('../src/viewport.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022 } }).outputText
const { viewportSize } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`)
for (const [width, height] of [[2048, 990], [1365, 767], [390, 844], [7680, 4320], [400, 2200]]) {
  const size = viewportSize(width, height)
  assert.ok(size.width >= 256 && size.width <= 3840)
  assert.ok(size.height >= 256 && size.height <= 2160)
  assert.equal(size.width % 8, 0)
  assert.equal(size.height % 2, 0)
  assert.ok(Math.abs(size.width / size.height / (width / height) - 1) < 0.02, 'window aspect ratio changed')
}
assert.deepEqual(viewportSize(1920, 1080), { type: 'resize', width: 1920, height: 1080 })
assert.deepEqual(viewportSize(1920, 1080, undefined, 2), { type: 'resize', width: 3840, height: 2160 })
assert.deepEqual(viewportSize(2560, 1440, undefined, 1.5), { type: 'resize', width: 3840, height: 2160 })
assert.deepEqual(viewportSize(3840, 2160, undefined, 2), { type: 'resize', width: 3840, height: 2160 })
assert.equal(viewportSize(0, 1080), null)
assert.equal(viewportSize(NaN, 1080), null)
assert.deepEqual(viewportSize(1920, 1080, { width: 1280, height: 720 }), { type: 'resize', width: 1280, height: 720 })
assert.deepEqual(viewportSize(2560, 1440, { width: 1280, height: 720 }), { type: 'resize', width: 1280, height: 720 })
const portraitCPU = viewportSize(390, 844, { width: 1280, height: 720 })
assert.ok(portraitCPU.height <= 720 && Math.abs(portraitCPU.width / portraitCPU.height / (390 / 844) - 1) < 0.02)

// Window drags send only their final size; reconnect can immediately resend it.
let callback, timer, nextTimer = 0, disconnected = false
globalThis.ResizeObserver = class {
  constructor(fn) { callback = fn }
  observe() {}
  disconnect() { disconnected = true }
}
globalThis.window = {
  setTimeout(fn) { timer = fn; return ++nextTimer },
  clearTimeout() { timer = undefined },
}
const { watchViewport } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`)
const element = { clientWidth: 1280, clientHeight: 800 }
const sent = []
const watcher = watchViewport(element, size => sent.push(size))
callback(); element.clientWidth = 1600; callback(); element.clientHeight = 900; callback()
assert.equal(sent.length, 0)
timer()
assert.deepEqual(sent, [{ type: 'resize', width: 1600, height: 900 }])
watcher.sync()
assert.equal(sent.length, 2)
callback(); watcher.stop()
assert.ok(disconnected)
assert.equal(timer, undefined)
let bounds = { width: 3840, height: 2160 }
const capped=[]
const cpuWatcher=watchViewport({clientWidth:1920,clientHeight:1080},size=>capped.push(size),()=>bounds)
cpuWatcher.sync()
bounds={width:1280,height:720}
cpuWatcher.sync()
assert.deepEqual(capped,[{type:'resize',width:1920,height:1080},{type:'resize',width:1280,height:720}])
cpuWatcher.stop()
console.log('Viewport bounds, aspect ratio, debounce, reconnect and cleanup passed.')
