import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

class Surface extends EventTarget {
  videoWidth = 1920; videoHeight = 1080; style = {}; srcObject = null
  focus() {} play() { return Promise.resolve() }
  getBoundingClientRect() { return { left: 0, top: 0, width: 1920, height: 1080 } }
}
const video = new Surface(), viewport = new Surface(), effects = [], sockets = [], peers = []
const frames = new Map(); let nextFrame = 0, refs = 0
globalThis.window = Object.assign(new Surface(), { location: { protocol:'http:',host:'localhost' },
  setInterval:()=>1,clearInterval(){},setTimeout:()=>1,clearTimeout(){},
  requestAnimationFrame:fn=>{frames.set(++nextFrame,fn);return nextFrame},cancelAnimationFrame:id=>frames.delete(id) })
globalThis.document = new Surface()
globalThis.WheelEvent = { DOM_DELTA_PIXEL:0, DOM_DELTA_LINE:1, DOM_DELTA_PAGE:2 }
globalThis.MediaStream = class { constructor(tracks){this.tracks=tracks} getTracks(){return this.tracks} }
globalThis.fetch = async()=>new Response(JSON.stringify({data:{resolution:'1920x1080'}}))
globalThis.WebSocket = class { static OPEN=1;readyState=1;constructor(){sockets.push(this)}send(){}close(){this.readyState=3} }
globalThis.RTCPeerConnection = class {
  connectionState='connected';signalingState='stable';transceivers=[{receiver:{track:{kind:'audio'}},direction:'recvonly'}]
  constructor(){peers.push(this)}
  createDataChannel(){return this.channel={readyState:'open',bufferedAmount:0,sent:[],send(b){this.sent.push(new DataView(b))},close(){this.readyState='closed'}}}
  getTransceivers(){return this.transceivers}
  async setRemoteDescription(d){this.remoteDescription=d} async createAnswer(){return {sdp:'answer'}} async setLocalDescription(){} close(){}
}
const source=await readFile(new URL('../src/components/PureStreamPlayer.tsx',import.meta.url),'utf8')
const js=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS,jsx:ts.JsxEmit.ReactJSX}}).outputText
const exports={}
const metricsSource=await readFile(new URL('../src/webrtc-performance.ts',import.meta.url),'utf8')
const metrics={};new Function('exports',ts.transpileModule(metricsSource,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText)(metrics)
new Function('require','exports',js)(name=>{
  if(name==='react')return {useRef:()=>({current:refs++===0?video:viewport}),useState:value=>[value,()=>{}],useEffect:fn=>effects.push(fn)}
  if(name==='react/jsx-runtime')return {jsx(){},jsxs(){}}
  if(name==='./ViewerPresentation')return {useViewerPresentation:()=>({showFPS:true})}
  if(name==='../viewport')return {watchViewport:()=>({sync(){},stop(){}})}
  if(name==='../webrtc-performance')return metrics
  throw new Error(name)
},exports)
exports.PureStreamPlayer({token:'test'})
const cleanup=effects[1]();await new Promise(r=>setImmediate(r))
const message=async m=>{sockets[0].onmessage({data:JSON.stringify(m)});await new Promise(r=>setImmediate(r))}
await message({event:'signal/provide',id:'viewer',sdp:'offer'})
await message({event:'control/locked',id:'viewer'})
const peer=peers[0], channel=peer.channel
const dispatch=(type,values={})=>{const e=new Event(type,{cancelable:true});Object.assign(e,{clientX:300,clientY:200,button:0,buttons:0,deltaX:0,deltaY:0,deltaMode:0,...values});video.dispatchEvent(e);return e}
const flush=()=>{const scheduled=[...frames.values()];frames.clear();for(const fn of scheduled)fn()}
const scrolls=()=>channel.sent.filter(d=>d.getUint8(0)===2).map(d=>[d.getInt16(3,true),d.getInt16(5,true)])
dispatch('wheel',{deltaY:120});flush()
assert.deepEqual(scrolls(),[[0,-1]],'DOM wheel down must map to one Linux down step, not pixels/up')
channel.sent=[]
for(let i=0;i<5;i++)dispatch('wheel',{deltaY:-20})
flush();assert.deepEqual(scrolls(),[[0,1]],'small touchpad deltas must accumulate without being discarded')
channel.sent=[]
dispatch('wheel',{deltaMode:1,deltaY:3});flush();dispatch('wheel',{deltaMode:1,deltaY:-3});flush()
assert.deepEqual(scrolls(),[[0,-1],[0,1]],'rapid direction changes must preserve both wheel operations')
channel.sent=[]
dispatch('wheel',{deltaMode:1,deltaY:3});dispatch('wheel',{deltaMode:1,deltaY:-3});flush()
assert.deepEqual(scrolls(),[[0,-1],[0,1]],'opposite wheel operations in the same display tick must both arrive')
channel.sent=[]
dispatch('wheel',{deltaX:100});flush();assert.deepEqual(scrolls(),[[-1,0]],'horizontal wheel must use Linux right button')
channel.sent=[]
for(let i=0;i<200;i++)dispatch('mousemove',{clientX:i+500})
assert.equal(channel.sent.length,0,'moves should wait for a display tick')
flush();assert.equal(channel.sent.length,1);assert.equal(channel.sent[0].getUint16(3,true),699)
dispatch('mousemove',{clientX:1000});dispatch('mousedown',{clientX:1001,buttons:1})
assert.equal(channel.sent.at(-1).getUint8(0),3,'latest position must precede button down')
assert.equal(channel.sent.at(-2).getUint16(3,true),1001)
channel.sent=[];channel.bufferedAmount=65536
dispatch('mousemove',{clientX:1500});flush();assert.equal(channel.sent.length,0,'congested channel must not queue stale movement')
channel.bufferedAmount=0;flush();assert.equal(channel.sent.at(-1).getUint16(3,true),1500)
dispatch('mousedown',{buttons:1});window.dispatchEvent(new Event('blur'))
assert.equal(channel.sent.at(-1).getUint8(0),4,'focus loss must release the mouse button')
const track={kind:'video'},receiver={jitterBufferTarget:100,playoutDelayHint:0.1}
peer.ontrack({track,receiver,streams:[new MediaStream([track,{kind:'audio'}])]})
assert.deepEqual(video.srcObject.getTracks(),[track],'muted viewer must display video without audio synchronization buffering')
assert.equal(receiver.jitterBufferTarget,0)
assert.equal(peer.transceivers[0].direction,'inactive','muted video player must decline unused audio')
const attached=video.srcObject;peer.ontrack({track:{kind:'audio'},receiver:{},streams:[]});assert.equal(video.srcObject,attached)
dispatch('mousemove',{clientX:1200});cleanup();flush()
assert.equal(frames.size,0,'disconnect must cancel pending movement')
console.log('Stream input direction, wheel accumulation, pointer order, congestion and video latency settings passed.')
