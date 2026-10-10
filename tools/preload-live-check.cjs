// Operate only on the project selected by the user. Credentials are read privately.
const fs=require('fs'),path=require('path'),crypto=require('crypto'),assert=require('node:assert/strict');
const dir=path.resolve(__dirname,'../dist/package-windows-amd64'),root=path.resolve(__dirname,'..');
const token=process.env.GOSTREAM3D_PROBE_TOKEN,base=process.env.GOSTREAM3D_PROBE_BASE_URL||'http://127.0.0.1:8888';
if(!token)throw Error('Set GOSTREAM3D_PROBE_TOKEN to the selected project token');
const expectedName=process.env.GOSTREAM3D_PROBE_PROJECT_NAME;
if(process.argv[2]==='configure'&&!expectedName)throw Error('Set GOSTREAM3D_PROBE_PROJECT_NAME before configuring a project');
const env=Object.fromEntries(fs.readFileSync(path.join(dir,'config.env'),'utf8').split(/\r?\n/).filter(l=>/^[A-Z0-9_]+=/.test(l.trim())).map(l=>{const i=l.indexOf('=');return[l.slice(0,i).trim(),l.slice(i+1).trim().replace(/^(['"])(.*)\1$/,'$2')]}));
(async()=>{
 const login=await fetch(base+'/api/v1/auth/login',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({username:env.GOSTREAM3D_ADMIN_USERNAME,password:env.GOSTREAM3D_ADMIN_PASSWORD})});assert(login.ok,'login failed');
 const headers={cookie:login.headers.get('set-cookie').split(';')[0],'content-type':'application/json'};
 const request=async(url,init={})=>{const r=await fetch(base+url,{...init,headers});if(!r.ok)throw Error('API '+r.status+': '+await r.text());return(await r.json()).data;};
 let sessions=await request('/api/v1/admin/sessions');let selected=sessions.find(s=>s.token===token);assert(selected,'Selected project missing');
 if(process.argv[2]==='configure'){
  assert.equal(selected.name,expectedName);assert.equal(selected.viewers,0);assert.equal(selected.max_viewers,6);
  const fields=['name','target_url','resolution','fps','render_mode','concurrency_mode','max_viewers','ready_selector'];
  const payload=Object.fromEntries(fields.map(f=>[f,selected[f]??'']));payload.preload_count=3;
  await request('/api/v1/admin/sessions/'+token,{method:'PUT',body:JSON.stringify(payload)});
  await request('/api/v1/admin/sessions/'+token+'/start?async=1',{method:'POST'});
  sessions=await request('/api/v1/admin/sessions');selected=sessions.find(s=>s.token===token);
 }
 const resources=await request('/api/v1/admin/resources'),channels=await request('/api/v1/admin/channels/settings');
 const baseline=JSON.parse(fs.readFileSync(path.join(root,'.cache/preload-runtime-before.json'),'utf8'));
 for(const [f,hash]of Object.entries(baseline.hashes)){assert.equal(fs.existsSync(path.join(dir,f))?crypto.createHash('sha256').update(fs.readFileSync(path.join(dir,f))).digest('hex'):null,hash,f+' changed');}
 for(const before of baseline.sessions){const after=sessions.find(s=>s.token===before.token);assert(after,'Original project missing');for(const f of ['token','name','target_url','resolution','fps','render_mode','concurrency_mode','max_viewers','ready_selector','created_at'])assert.equal(after[f],before[f],f+' unexpectedly changed');if(after.token!==token)assert.equal(after.render_paused,before.render_paused,'Other project state changed');}
 const summary={name:'示例项目 A',quota:selected.max_viewers,preload:selected.preload_count,ready:selected.ready_instances,available:selected.available_instances,starting:selected.starting_instances,instances:selected.running_instances,viewers:selected.viewers,paused:selected.render_paused,preload_error:selected.preload_error||'',pool_error:selected.pool_error||'',channels,cpu_max_percent:resources.cpu_max_percent};
 if(process.argv[2]==='frames'){
  assert.equal(selected.ready_instances,3);assert.equal(selected.available_instances,3);
  const firstFrames=[];const connections=[];
  try {
   for(let route=1;route<=3;route++){
    const result=await new Promise((resolve,reject)=>{
     const started=performance.now();const socket=new WebSocket(base.replace('http','ws')+'/api/v1/ws/'+token);connections.push(socket);socket.binaryType='arraybuffer';
     const timeout=setTimeout(()=>reject(Error('route '+route+' first-frame timeout')),15000);
     socket.onerror=()=>{clearTimeout(timeout);reject(Error('route '+route+' socket error'))};
     socket.onmessage=event=>{if(!(event.data instanceof ArrayBuffer))return;clearTimeout(timeout);socket.onmessage=null;const frame=Buffer.from(event.data);assert(frame.length>1000);assert.equal(frame.readUInt16BE(0),0xffd8);resolve({route,first_frame_ms:Math.round(performance.now()-started),bytes:frame.length});};
    });firstFrames.push(result);
   }
   const occupied=(await request('/api/v1/admin/sessions')).find(s=>s.token===token);assert.equal(occupied.viewers,3);assert.equal(occupied.ready_instances,3);assert.equal(occupied.available_instances,0);
   summary.firstFrames=firstFrames;summary.simultaneous_viewers=occupied.viewers;
  }finally{connections.forEach(s=>s.close());}
  fs.writeFileSync(path.join(root,'.cache/preload-live-report.json'),JSON.stringify(summary,null,2));
 }
 console.log(JSON.stringify(summary));
})().catch(e=>{console.error(e);process.exitCode=1});
