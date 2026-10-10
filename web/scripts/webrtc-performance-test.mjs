import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import ts from 'typescript'
const source=await readFile(new URL('../src/webrtc-performance.ts',import.meta.url),'utf8')
const js=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
const e={};new Function('exports',js)(e)
const previous={id:'video',timestamp:1000,framesReceived:30,framesDecoded:28,framesDropped:2,bytesReceived:100000,totalDecodeTime:.14,jitterBufferDelay:.28,jitterBufferEmittedCount:28,packetsLost:1,packetsReceived:1000}
const current={...previous,timestamp:4000,framesReceived:120,framesDecoded:115,framesDropped:5,bytesReceived:2350000,totalDecodeTime:.575,jitterBufferDelay:1.15,jitterBufferEmittedCount:115,packetsLost:4,packetsReceived:4000}
const playback={at:4000,total:120,dropped:5,width:1920,height:1080}
const oldPlayback={...playback,at:1000,total:30,dropped:2}
const m=e.webrtcPerformance(previous,current,oldPlayback,playback,'video/H264',.015)
assert.equal(m.receive_fps,30);assert.equal(m.display_fps,29);assert.equal(m.bitrate_mbps,6)
assert.ok(Math.abs(m.decode_ms-5)<1e-8);assert.ok(Math.abs(m.buffer_ms-10)<1e-8)
assert.equal(m.rtt_ms,15);assert.equal(m.packets_lost,3);assert.equal(m.frames_dropped,3)
assert.equal(e.webrtcPerformance(undefined,current,undefined,playback,'video/H264',0),null,'first sample establishes a baseline')
assert.equal(e.webrtcPerformance(previous,{...current,id:'new'},oldPlayback,playback,'video/H264',0),null,'SSRC change resets counters')
assert.equal(e.webrtcPerformance(previous,{...current,bytesReceived:0},oldPlayback,playback,'video/H264',0),null,'reset cannot fabricate a rate')
assert.equal(e.webrtcPerformance(previous,{...current,timestamp:1000},oldPlayback,playback,'video/H264',0),null)
const missing=e.webrtcPerformance({id:'x',timestamp:1000,bytesReceived:0},{id:'x',timestamp:4000,bytesReceived:0},undefined,playback,'video/H264',undefined)
assert.equal(missing.bitrate_mbps,0);assert.equal(missing.receive_fps,null);assert.equal(missing.decode_ms,null);assert.equal(missing.buffer_ms,null);assert.equal(missing.rtt_ms,null)
assert.equal(e.webrtcPerformance(previous,{...current,framesDecoded:previous.framesDecoded,totalDecodeTime:previous.totalDecodeTime},oldPlayback,playback,'video/H264',0).decode_ms,null,'no decoded frames is unavailable, not a fabricated zero cost')
console.log('WebRTC interval metrics, unknown/zero distinction, SSRC resets and first-sample baseline passed.')
