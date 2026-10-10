// Run from the repository root with Node 22+. Uses an idle, paused CPU link.
// Counts actual JPEG frames without attaching another DevTools capture client.
const fs = require('fs');
const { performance } = require('perf_hooks');
const base = process.env.BENCHMARK_BASE_URL || 'http://127.0.0.1:8888';
const cfg = Object.fromEntries(fs.readFileSync('dist/package-windows-amd64/config.env', 'utf8').split(/\r?\n/).filter(x => /^GOSTREAM3D_\w+=/.test(x)).map(x => { const i = x.indexOf('='); return [x.slice(0, i), x.slice(i + 1)]; }));
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
async function main() {
  const login = await fetch(base + '/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: cfg.GOSTREAM3D_ADMIN_USERNAME, password: cfg.GOSTREAM3D_ADMIN_PASSWORD }), signal: AbortSignal.timeout(10000) });
  if (!login.ok) throw new Error('Login failed');
  const headers = { Cookie: login.headers.getSetCookie().map(x => x.split(';')[0]).join('; ') };
  const rows = (await (await fetch(base + '/api/v1/admin/sessions', { headers })).json()).data;
  const item = rows.find(x => x.render_mode === 'cpu' && x.target_url.includes('campus.thingjs.com') && x.viewers === 0 && x.render_paused);
  if (!item) throw new Error('Expected an idle, paused campus CPU session');
  let viewer, timer, diagnostic;
  try {
    console.log('Warming CPU scene under the configured CPU cap...');
    const start = await fetch(base + `/api/v1/admin/sessions/${item.token}/start`, { method: 'POST', headers, signal: AbortSignal.timeout(310000) });
    if (!start.ok) throw new Error('CPU scene preload failed: ' + start.status);
    if (process.env.BENCHMARK_DIAGNOSTICS === '1') {
      const targets = await (await fetch('http://127.0.0.1:31000/json/list')).json();
      diagnostic = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
      await new Promise(resolve => diagnostic.addEventListener('open', resolve, { once: true }));
      let id = 0;
      const pending = new Map();
      diagnostic.addEventListener('message', event => { const m = JSON.parse(event.data); if (pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); } });
      diagnostic.request = (method, params = {}) => new Promise((resolve, reject) => {
        const n = ++id, timeout = setTimeout(() => { pending.delete(n); reject(new Error('Diagnostic evaluation timed out')); }, 10000);
        pending.set(n, value => { clearTimeout(timeout); resolve(value); });
        diagnostic.send(JSON.stringify({ id: n, method, params }));
      });
      diagnostic.evaluate = expression => diagnostic.request('Runtime.evaluate', { expression, returnByValue: true });
      await diagnostic.evaluate(`window.__g3dBenchEvents={down:0,move:0,up:0,wheel:0};window.__g3dBenchErrors=[];window.addEventListener('error',e=>window.__g3dBenchErrors.push(e.message));for(const [event,key] of [['mousedown','down'],['mousemove','move'],['mouseup','up'],['wheel','wheel']])document.addEventListener(event,()=>window.__g3dBenchEvents[key]++,{capture:true});true`);
    }
    viewer = new WebSocket(base.replace(/^http/, 'ws') + `/api/v1/ws/${item.token}`);
    viewer.binaryType = 'arraybuffer';
    let frames = 0, bytes = 0, timestamps = [], unique = new Set(), lastFrame, unexpectedClose = false;
    viewer.addEventListener('close', () => { unexpectedClose = true; });
    const crypto = require('crypto');
    viewer.addEventListener('message', event => {
      if (typeof event.data === 'string') return;
      frames++; bytes += event.data.byteLength;
      unique.add(crypto.createHash('sha256').update(Buffer.from(event.data)).digest('hex'));
      lastFrame = Buffer.from(event.data);
      timestamps.push(performance.now());
    });
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Viewer connection timed out')), 30000);
      viewer.addEventListener('open', () => { clearTimeout(timeout); resolve(); }, { once: true });
      viewer.addEventListener('error', () => { clearTimeout(timeout); reject(new Error('Viewer connection failed')); }, { once: true });
    });
    await sleep(Number(process.env.BENCHMARK_SETTLE_MS || 2000));
    if (diagnostic) console.log('Before input: ' + (await diagnostic.evaluate(`JSON.stringify({frame:app._currentFrame,fps:app.renderStates.fps,triangles:app.renderStates.triangles,center:document.elementFromPoint(960,540)?.tagName,context:app.renderer.getContextAttributes(),effects:window.__g3dCPUEffects||null,postEffect:app.postEffect!==null,shadows:app.renderer.shadowMap.enabled,outlines:app.rendererManager?._mainRenderer?.enableOutlines,camera:app.camera.position,renderCamera:app.renderCamera.position})`)).result?.result?.value);
    frames = 0; bytes = 0; timestamps = []; unique.clear();
    const [width, height] = item.resolution.split('x').map(Number);
    if (diagnostic && process.env.BENCHMARK_PROFILE_PATH) {
      await diagnostic.request('Profiler.enable');
      await diagnostic.request('Profiler.start');
    }
    const begin = performance.now();
    viewer.send(JSON.stringify({ type: 'down', x: width / 2, y: height / 2, button: 'left', buttons: 1 }));
    timer = setInterval(() => {
      const elapsed = performance.now() - begin;
      viewer.send(JSON.stringify({ type: 'move', x: width / 2 + Math.sin(elapsed / 900) * 220, y: height / 2 + Math.cos(elapsed / 1100) * 70, button: 'none', buttons: 1 }));
    }, 33);
    await sleep(12000); clearInterval(timer);
    viewer.send(JSON.stringify({ type: 'up', x: width / 2, y: height / 2, button: 'left', buttons: 0 }));
    const seconds = (performance.now() - begin) / 1000;
    const gaps = timestamps.slice(1).map((t, i) => t - timestamps[i]).sort((a, b) => a - b);
    console.log(JSON.stringify({ label: process.env.BENCHMARK_LABEL || 'cpu', resolution: item.resolution, requested_fps: item.fps, seconds: +seconds.toFixed(2), frames, unique_frames: unique.size, stream_fps: +(frames / seconds).toFixed(2), avg_frame_kb: frames ? +(bytes / frames / 1024).toFixed(1) : 0, p95_frame_gap_ms: gaps.length ? +gaps[Math.floor(gaps.length * .95)].toFixed(1) : null, unexpected_close: unexpectedClose }));
    if (diagnostic) {
      await sleep(500);
      console.log('After input: ' + (await diagnostic.evaluate(`JSON.stringify({events:window.__g3dBenchEvents,frame:app._currentFrame,fps:app.renderStates.fps,camera:app.camera.position,renderCamera:app.renderCamera.position})`)).result?.result?.value);
      if (process.env.BENCHMARK_PROFILE_PATH) {
        const result = await diagnostic.request('Profiler.stop');
        if (!result.result?.profile) throw new Error('No CPU profile returned');
        fs.writeFileSync(process.env.BENCHMARK_PROFILE_PATH, JSON.stringify(result.result.profile));
      }
      if (process.env.BENCHMARK_INTERACTION === '1') {
        const state = async () => (await diagnostic.evaluate(`JSON.stringify({camera:app.camera.position,target:app.camera.target,events:window.__g3dBenchEvents,errors:window.__g3dBenchErrors,body:document.body.innerText.slice(0,500),postEffect:app.postEffect?.postEffect?.enable})`)).result?.result?.value;
        console.log('Before zoom: ' + await state());
        viewer.send(JSON.stringify({ type: 'wheel', x: width / 2, y: height / 2, deltaY: -120, deltaX: 0 }));
        await sleep(5000);
        console.log('Zoom in: ' + await state());
        viewer.send(JSON.stringify({ type: 'wheel', x: width / 2, y: height / 2, deltaY: 120, deltaX: 0 }));
        await sleep(5000);
        console.log('Zoom out: ' + await state());
        viewer.send(JSON.stringify({ type: 'down', x: 25, y: 25, button: 'left', buttons: 1 }));
        viewer.send(JSON.stringify({ type: 'up', x: 25, y: 25, button: 'left', buttons: 0 }));
        await sleep(3000);
        console.log('Panel click: ' + await state());
      }
    }
    if (process.env.BENCHMARK_FRAME_PATH && lastFrame) fs.writeFileSync(process.env.BENCHMARK_FRAME_PATH, lastFrame);
  } finally {
    clearInterval(timer);
    viewer?.close();
    diagnostic?.close();
    await sleep(300);
    await fetch(base + `/api/v1/admin/sessions/${item.token}/stop`, { method: 'POST', headers, signal: AbortSignal.timeout(20000) });
    console.log('Restored the session to paused.');
  }
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
