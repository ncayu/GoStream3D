// Temporary, isolated CPU scene: compare the source render rate and delivered
// JPEG rate before and after diagnostic-only scene changes. Delete on exit.
const fs = require('node:fs')
const config = Object.fromEntries(fs.readFileSync('dist/package-windows-amd64/config.env', 'utf8').split(/\r?\n/).filter(line => /^GOSTREAM3D_\w+=/.test(line)).map(line => { const i = line.indexOf('='); return [line.slice(0, i), line.slice(i + 1)] }))
const base = 'http://127.0.0.1:8888'
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))

async function main() {
  const login = await fetch(base + '/api/v1/auth/login', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({username: config.GOSTREAM3D_ADMIN_USERNAME, password: config.GOSTREAM3D_ADMIN_PASSWORD})})
  if (!login.ok) throw Error('login failed')
  const headers = {Cookie: login.headers.getSetCookie().map(cookie => cookie.split(';')[0]).join('; ')}
  const sessions = (await (await fetch(base + '/api/v1/admin/sessions', {headers})).json()).data
  const source = sessions.find(item => item.render_mode === 'cpu' && item.target_url.includes('campus.thingjs.com') && item.render_paused)
  if (!source) throw Error('paused campus CPU project unavailable')
  const create = await fetch(base + '/api/v1/sessions?async=1', {method: 'POST', headers: {...headers, 'Content-Type': 'application/json'}, body: JSON.stringify({name: 'CPU 场景性能临时测试', target_url: source.target_url, resolution: source.resolution, fps: source.fps, render_mode: 'cpu', concurrency_mode: 'single', max_viewers: 1, preload_count: 1, ready_selector: source.ready_selector || ''})})
  if (!create.ok) throw Error('create test project failed: ' + create.status + ' ' + await create.text())
  const token = (await create.json()).data.token
  let browser, viewer, timer
  try {
    const deadline = Date.now() + 240000
    while (Date.now() < deadline) {
      const row = (await (await fetch(base + '/api/v1/admin/sessions', {headers})).json()).data.find(item => item.token === token)
      if (row?.ready_instances > 0) break
      if (row?.preload_error) throw Error('preload failed: ' + row.preload_error)
      await sleep(1000)
    }
    if (Date.now() >= deadline) throw Error('CPU preload timed out')
    const pages = await (await fetch('http://127.0.0.1:31000/json/list')).json()
    const page = pages.find(item => item.type === 'page' && item.webSocketDebuggerUrl)
    if (!page) throw Error('CDP page unavailable')
    browser = new WebSocket(page.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => { browser.onopen = resolve; browser.onerror = reject })
    let id = 0
    const pending = new Map()
    browser.onmessage = event => {
      const message = JSON.parse(event.data)
      const entry = pending.get(message.id)
      if (!entry) return
      pending.delete(message.id)
      message.error ? entry.reject(Error(message.error.message)) : entry.resolve(message.result?.result?.value)
    }
    const evaluate = expression => new Promise((resolve, reject) => {
      const next = ++id
      pending.set(next, {resolve, reject})
      browser.send(JSON.stringify({id: next, method: 'Runtime.evaluate', params: {expression, returnByValue: true}}))
      setTimeout(() => { if (pending.delete(next)) reject(Error('CDP timed out')) }, 15000)
    })
    console.log('Renderer: ' + await evaluate(`(() => { const gl = app.renderer.getContext(), ext = gl.getExtension('WEBGL_debug_renderer_info'); return ext ? gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER); })()`))
    viewer = new WebSocket(base.replace(/^http/, 'ws') + '/api/v1/ws/' + token)
    viewer.binaryType = 'arraybuffer'
    await new Promise((resolve, reject) => { viewer.onopen = resolve; viewer.onerror = reject })
    const samples = []
    viewer.onmessage = event => { if (event.data instanceof ArrayBuffer) samples.push(performance.now()) }
    await sleep(3000)
    const measure = async (name, setup) => {
      if (setup) console.log(name + ' setup: ' + await evaluate(setup))
      await sleep(1500)
      const before = Number(await evaluate('app._currentFrame'))
      const first = performance.now()
      viewer.send(JSON.stringify({type: 'down', x: 960, y: 540, button: 'left', buttons: 1}))
      timer = setInterval(() => viewer.send(JSON.stringify({type: 'move', x: 960 + Math.sin(performance.now() / 900) * 220, y: 540, button: 'none', buttons: 1})), 33)
      await sleep(12000)
      clearInterval(timer)
      timer = undefined
      viewer.send(JSON.stringify({type: 'up', x: 960, y: 540, button: 'left', buttons: 0}))
      const end = performance.now()
      const after = Number(await evaluate('app._currentFrame'))
      const count = samples.filter(time => time >= first && time <= end).length
      console.log(JSON.stringify({name, seconds: Number(((end - first) / 1000).toFixed(2)), source_frames: after - before, delivered_frames: count, delivered_fps: Number((count / ((end - first) / 1000)).toFixed(2)), render_state: await evaluate('JSON.stringify(app.renderStates)'), interaction: await evaluate('JSON.stringify({picker:app.picker?.enable,camera:app.camera.position})')}))
    }
    if (process.argv[2] === 'no-capture') {
      viewer.send(JSON.stringify({type: 'inactive'}))
      await sleep(1000)
      await measure('diagnostic-no-capture')
    } else if (process.argv[2] === 'picker-off') {
      await measure('picker-disabled-during-drag', `(() => { const before=app.picker.enable; app.picker.enable=false; return JSON.stringify({before,after:app.picker.enable}); })()`)
    } else if (process.argv[2] === 'picker-drag') {
      await measure('picker-suspended-only-while-dragging', `(() => {
        let down = null, suspended = false, moves = 0;
        document.addEventListener('mousedown', e => { if (e.button === 0 && e.target instanceof HTMLCanvasElement) down = {x:e.clientX,y:e.clientY}; }, true);
        document.addEventListener('mousemove', e => {
          if (!down || !(e.buttons & 1)) return;
          moves++;
          if (!suspended && Math.hypot(e.clientX-down.x,e.clientY-down.y) > 4) { app.picker.enable=false; suspended=true; }
        }, true);
        document.addEventListener('mouseup', () => { down=null; if (suspended) { app.picker.enable=true; suspended=false; } }, true);
        window.__g3dPickerDiagnostic = () => ({down:!!down,suspended,moves,picker:app.picker.enable});
        return JSON.stringify({picker:app.picker.enable, canvases:document.querySelectorAll('canvas').length});
      })()`)
      console.log('Picker diagnostic: ' + await evaluate('JSON.stringify(window.__g3dPickerDiagnostic())'))
    } else if (process.argv[2] === 'baseline-only') {
      await measure('baseline-only')
    } else if (process.argv[2] !== 'no-mesh') {
    await measure('baseline')
    await measure('basic-material', `(() => {
      const replacements = new Map(); let changed = 0;
      app.scene.traverse(node => {
        if (!node.isMesh || !node.material) return;
        const replace = old => {
          if (!old || old.isMeshBasicMaterial) return old;
          if (!replacements.has(old)) replacements.set(old, new THREE.MeshBasicMaterial({map: old.map || null, color: old.color || 0xffffff, transparent: old.transparent, opacity: old.opacity, alphaTest: old.alphaTest, side: old.side, vertexColors: old.vertexColors, depthWrite: old.depthWrite}));
          return replacements.get(old);
        };
        node.userData.__g3dOriginalMaterial = node.material;
        node.material = Array.isArray(node.material) ? node.material.map(replace) : replace(node.material);
        changed++;
      });
      return changed;
    })()`)
    await measure('quarter-geometry', `(() => {
      let restored = 0, reduced = 0; const seen = new Set();
      app.scene.traverse(node => {
        if (node.userData?.__g3dOriginalMaterial) { node.material = node.userData.__g3dOriginalMaterial; delete node.userData.__g3dOriginalMaterial; restored++; }
        const g = node.geometry;
        if (!g?.setDrawRange || seen.has(g)) return;
        seen.add(g);
        const count = g.index?.count || g.attributes?.position?.count || 0;
        if (count > 3000) { g.setDrawRange(0, Math.floor(count / 12) * 3); reduced++; }
      });
      return JSON.stringify({restored, reduced});
    })()`)
    }
    if (!['no-capture', 'baseline-only', 'picker-off', 'picker-drag'].includes(process.argv[2])) await measure('diagnostic-no-mesh', `(() => {
      let hidden = 0;
      app.scene.traverse(node => { if (node.isMesh && node.visible) { node.visible = false; hidden++; } });
      return hidden;
    })()`)
  } finally {
    clearInterval(timer)
    viewer?.close()
    browser?.close()
    await sleep(200)
    const response = await fetch(base + '/api/v1/admin/sessions/' + token, {method: 'DELETE', headers, signal: AbortSignal.timeout(30000)})
    console.log('Deleted temporary project: ' + response.status)
  }
}
main().catch(error => { console.error(error); process.exitCode = 1 })
