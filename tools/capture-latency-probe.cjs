const assert=require('node:assert/strict');
async function sample(port){
 const pages=await (await fetch('http://127.0.0.1:'+port+'/json/list')).json();const t=pages.find(p=>p.type==='page'&&p.webSocketDebuggerUrl);assert(t,'no page '+port);
 const ws=new WebSocket(t.webSocketDebuggerUrl);let seq=0;const awaiting=new Map();
 await new Promise((ok,bad)=>{ws.onopen=ok;ws.onerror=bad});ws.onmessage=e=>{const v=JSON.parse(e.data);const x=awaiting.get(v.id);if(x){awaiting.delete(v.id);v.error?x.bad(Error(v.error.message)):x.ok(v.result)}};
 async function call(method,params){return new Promise((ok,bad)=>{const id=++seq;awaiting.set(id,{ok,bad});ws.send(JSON.stringify({id,method,params}));setTimeout(()=>{if(awaiting.delete(id))bad(Error('timeout '+method))},15000)})}
 const result=[];try{for(let i=0;i<3;i++){const start=performance.now();const out=await call('Page.captureScreenshot',{format:'jpeg',quality:75,optimizeForSpeed:true,fromSurface:true,captureBeyondViewport:false,clip:{x:0,y:0,width:1920,height:1080,scale:1}});result.push({ms:Math.round(performance.now()-start),bytes:Math.round((out.data?.length||0)*.75)})}}finally{ws.close()};return {port,result};
}
(async()=>{console.log(JSON.stringify(await Promise.all([31000,31001,31002].map(sample))))})().catch(e=>{console.error(e);process.exitCode=1});
