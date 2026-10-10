import {createRequire} from 'node:module'
import {writeFile} from 'node:fs/promises'
// Run only against a disposable test project: the measurement pans the scene.
const {chromium}=createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const [url,label]=process.argv.slice(2)
const browser=await chromium.launch({channel:'msedge',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1920,height:1080}})
 await page.addInitScript(()=>{const Original=window.RTCPeerConnection;window.__benchPeers=[];window.RTCPeerConnection=class extends Original{constructor(...args){super(...args);window.__benchPeers.push(this)}}})
 await page.goto(url)
 await page.waitForFunction(()=>{const v=document.querySelector('video');return v?.videoWidth>0 && v.getVideoPlaybackQuality().totalVideoFrames>20},{},{timeout:120000})
 await page.waitForTimeout(5000)
 const snapshot=()=>page.evaluate(async()=>{const v=document.querySelector('video');const q=v.getVideoPlaybackQuality();const stats=[];for(const p of window.__benchPeers){const all=await p.getStats();for(const r of all.values())if(r.type==='inbound-rtp'&&r.kind==='video')stats.push({codec:all.get(r.codecId)?.mimeType,framesDecoded:r.framesDecoded,framesDropped:r.framesDropped,totalDecodeTime:r.totalDecodeTime,jitterBufferDelay:r.jitterBufferDelay,jitterBufferEmittedCount:r.jitterBufferEmittedCount,packetsLost:r.packetsLost,framesReceived:r.framesReceived,bytesReceived:r.bytesReceived})}return{time:performance.now(),width:v.videoWidth,height:v.videoHeight,total:q.totalVideoFrames,dropped:q.droppedVideoFrames,stats}})
 const samples=[];let previous=await snapshot()
 console.log('MEASURING',label,previous.width,previous.height)
 for(let i=0;i<5;i++){
  await page.mouse.move(960,540);await page.mouse.down();await page.mouse.move(1100,580,{steps:12});await page.mouse.up()
  await page.waitForTimeout(6000)
  const current=await snapshot();samples.push({seconds:(current.time-previous.time)/1000,fps:(current.total-current.dropped-previous.total+previous.dropped)*1000/(current.time-previous.time),width:current.width,height:current.height,before:previous.stats,after:current.stats});previous=current
 }
 const report={label,url,samples,averageFPS:samples.reduce((a,s)=>a+s.fps*s.seconds,0)/samples.reduce((a,s)=>a+s.seconds,0)}
 await page.screenshot({path:`.cache/cpu-benchmark-${label}.png`})
 await writeFile(`.cache/cpu-benchmark-${label}.json`,JSON.stringify(report,null,2))
 console.log(JSON.stringify({label,averageFPS:report.averageFPS,sampleFPS:samples.map(x=>x.fps),width:previous.width,height:previous.height}))
}finally{await browser.close()}
