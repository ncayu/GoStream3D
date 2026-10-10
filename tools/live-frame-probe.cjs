const assert=require('node:assert/strict');
const ports=[31000,31001,31002];
async function probe(port){
 const pages=await (await fetch('http://127.0.0.1:'+port+'/json/list')).json();
 const target=pages.find(t=>t.type==='page'&&t.webSocketDebuggerUrl);assert(target,'missing page '+port);
 const socket=new WebSocket(target.webSocketDebuggerUrl);let id=0;const pending=new Map();
 await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject;});
 socket.onmessage=e=>{const v=JSON.parse(e.data);if(!v.id)return;const p=pending.get(v.id);if(p){pending.delete(v.id);v.error?p.reject(new Error(v.error.message)):p.resolve(v.result);}};
 const call=(method,params={})=>new Promise((resolve,reject)=>{const n=++id;pending.set(n,{resolve,reject});socket.send(JSON.stringify({id:n,method,params}));setTimeout(()=>{if(pending.delete(n))reject(new Error('CDP timeout '+method))},5000);});
 const sample=async()=>{const start=performance.now();const result=await call('Runtime.evaluate',{expression:'JSON.stringify({frame:window.app?window.app._currentFrame:null,visibility:document.visibilityState,width:innerWidth,height:innerHeight,ready:document.readyState})',returnByValue:true});return {...JSON.parse(result.result.value),eval_ms:Math.round(performance.now()-start)};};
 return {socket,sample};
}
(async()=>{const results=await Promise.all(ports.map(probe));try{const first=await Promise.all(results.map(x=>x.sample()));await new Promise(r=>setTimeout(r,6000));const second=await Promise.all(results.map(x=>x.sample()));console.log(JSON.stringify(ports.map((port,i)=>({port,before:first[i],after:second[i],app_fps:typeof first[i].frame==='number'&&typeof second[i].frame==='number'?Number(((second[i].frame-first[i].frame)/6).toFixed(1)):null}))));}finally{results.forEach(x=>x.socket.close());}})().catch(e=>{console.error(e);process.exitCode=1});
