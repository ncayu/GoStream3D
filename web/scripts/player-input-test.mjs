import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

class Surface extends EventTarget {
  style = {}
  width = 1280
  height = 720
  focus() {}
  getContext() { return { drawImage() {} } }
  getBoundingClientRect() { return { left: 0, top: 0, width: 1280, height: 720 } }
}
const canvas = new Surface(), viewport = new Surface()
let time = 1000
globalThis.performance = { now: () => time++ }
globalThis.window = Object.assign(new Surface(), {
  location: { protocol: 'http:', host: 'localhost' },
  setInterval: () => 1, clearInterval() {},
  setTimeout: () => 1, clearTimeout() {},
  requestAnimationFrame: () => 1, cancelAnimationFrame() {},
})
globalThis.document = Object.assign(new Surface(), { visibilityState: 'visible' })
globalThis.fetch = async () => new Response(JSON.stringify({ data: { resolution: '1280x720', running: true } }))
globalThis.createImageBitmap = async () => ({ width: 1280, height: 720, close() {} })
const sockets = []
globalThis.WebSocket = class {
  static OPEN = 1
  readyState = 1
  sent = []
  constructor() { sockets.push(this) }
  send(value) { this.sent.push(JSON.parse(value)) }
  close() { this.readyState = 3 }
}
let refIndex = 0
const effects = []
const source = await readFile(new URL('../src/components/LocalCanvasPlayer.tsx', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } }).outputText
const exports = {}
new Function('require', 'exports', compiled)(name => {
  if (name === 'react') return {
    useRef: () => ({ current: refIndex++ === 0 ? canvas : viewport }),
    useState: value => [value, () => {}], useEffect: effect => effects.push(effect),
  }
  if (name === 'react/jsx-runtime') return { jsx() {}, jsxs() {} }
  if (name === './ViewerPresentation') return { useViewerPresentation: () => ({}) }
  if (name === '../viewport') return { watchViewport: () => ({ sync() {}, stop() {} }) }
  throw new Error(`Unexpected import: ${name}`)
}, exports)
exports.LocalCanvasPlayer({ token: 'test' })
const cleanup = effects[0]()
await new Promise(resolve => setImmediate(resolve))
const socket = sockets[0]
socket.onopen()
socket.onmessage({ data: new Blob(['frame']) })
await new Promise(resolve => setImmediate(resolve))
socket.onmessage({ data: '{"type":"frame","id":9}' })
socket.onmessage({ data: new Blob(['frame']) })
await new Promise(resolve => setImmediate(resolve))
assert.equal(socket.sent.at(-1).type, 'frameAck')
assert.equal(socket.sent.at(-1).frameID, 9)
assert.ok(socket.sent.at(-1).decodeMS >= 0 && socket.sent.at(-1).drawMS >= 0)

const key = (type, key, code, extra = {}) => {
  const event = new Event(type, { cancelable: true })
  Object.assign(event, { key, code, keyCode: 0, location: 0, repeat: false, ...extra })
  canvas.dispatchEvent(event)
  return event.defaultPrevented
}
for (const [name, code, extra] of [
  ['F11', 'F11', {}], ['F5', 'F5', {}], ['F12', 'F12', {}],
  ['r', 'KeyR', { ctrlKey: true }], ['l', 'KeyL', { ctrlKey: true }],
  ['Tab', 'Tab', {}], ['ArrowLeft', 'ArrowLeft', { altKey: true }],
  ['r', 'KeyR', { metaKey: true }],
]) {
  const before = socket.sent.length
  assert.equal(key('keydown', name, code, extra), false, `${code} browser shortcut intercepted`)
  assert.equal(socket.sent.length, before, `${code} forwarded to scene`)
}
const beforeNormalKey = socket.sent.length
key('keydown', 'd', 'KeyD')
const inputProbe = socket.sent.at(-1)
assert.ok(inputProbe.inputID > 0, 'scene input did not carry a sampled sequence')
socket.onmessage({ data: JSON.stringify({ type: 'frame', id: 10, inputID: inputProbe.inputID }) })
socket.onmessage({ data: new Blob(['frame']) })
await new Promise(resolve => setImmediate(resolve))
assert.ok(socket.sent.at(-1).inputMS > 0, 'next displayed frame did not report sampled input latency')
socket.sent.pop() // Keep the key lifecycle assertion independent of telemetry.
key('keydown', 'd', 'KeyD', { repeat: true })
key('keyup', 'd', 'KeyD', { ctrlKey: true })
assert.deepEqual(socket.sent.slice(beforeNormalKey).map(e => e.type), ['keyDown', 'keyDown', 'keyUp'])
assert.equal(socket.sent.at(-1).text, '')
const beforeIME = socket.sent.length
assert.equal(key('keydown', 'Process', 'KeyA', { isComposing: true }), false)
assert.equal(socket.sent.length, beforeIME)
const beforeOrphanRepeat = socket.sent.length
key('keydown', 'w', 'KeyW', { repeat: true })
assert.equal(socket.sent.length, beforeOrphanRepeat, 'repeat rearmed a key cleared on focus loss')
socket.sent.length = 0
assert.equal(key('keydown', 'ArrowUp', 'ArrowUp'), true)
key('keydown', 'w', 'KeyW')
window.dispatchEvent(new Event('blur'))
assert.deepEqual(socket.sent.filter(e => e.type === 'keyUp').map(e => e.code), ['KeyW', 'ArrowUp'])
const released = socket.sent.length
window.dispatchEvent(new Event('blur'))
assert.equal(socket.sent.length, released, 'duplicate releases after blur')
key('keydown', 'ArrowLeft', 'ArrowLeft')
document.visibilityState = 'hidden'
document.dispatchEvent(new Event('visibilitychange'))
assert.deepEqual(socket.sent.slice(-2).map(e => e.type), ['keyUp', 'inactive'])
document.visibilityState = 'visible'
key('keydown', 'Shift', 'ShiftLeft', { shiftKey: true })
key('keydown', 'A', 'KeyA', { shiftKey: true })
key('keydown', 'r', 'KeyR', { ctrlKey: true })
assert.deepEqual(socket.sent.slice(-2).map(e => e.code), ['KeyA', 'ShiftLeft'])
key('keydown', 'ArrowDown', 'ArrowDown')
canvas.dispatchEvent(new Event('blur'))
assert.equal(socket.sent.at(-1).type, 'keyUp', 'canvas focus loss did not release key')
key('keydown', 'ArrowRight', 'ArrowRight')
cleanup()
assert.equal(socket.sent.at(-1).type, 'keyUp', 'unmount did not release key before closing')
console.log('Player browser shortcuts and keyboard release lifecycle passed.')
