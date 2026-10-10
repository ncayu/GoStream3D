// Inspect an idle, paused campus CPU worker and restore it to paused afterwards.
const fs = require('node:fs')
const config = Object.fromEntries(fs.readFileSync('dist/package-windows-amd64/config.env', 'utf8').split(/\r?\n/).filter(line => /^GOSTREAM3D_\w+=/.test(line)).map(line => { const i = line.indexOf('='); return [line.slice(0, i), line.slice(i + 1)] }))
const base = 'http://127.0.0.1:8888'
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))

async function main() {
  const login = await fetch(base + '/api/v1/auth/login', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({username: config.GOSTREAM3D_ADMIN_USERNAME, password: config.GOSTREAM3D_ADMIN_PASSWORD})})
  if (!login.ok) throw Error('login failed')
  const headers = {Cookie: login.headers.getSetCookie().map(cookie => cookie.split(';')[0]).join('; ')}
  const sessions = (await (await fetch(base + '/api/v1/admin/sessions', {headers})).json()).data
  const session = sessions.find(item => item.render_mode === 'cpu' && item.target_url.includes('campus.thingjs.com') && item.render_paused && item.viewers === 0)
  if (!session) throw Error('paused campus CPU project unavailable')
  let browser
  try {
    const response = await fetch(base + '/api/v1/admin/sessions/' + session.token + '/start', {method: 'POST', headers, signal: AbortSignal.timeout(310000)})
    if (!response.ok) throw Error('preload failed: ' + response.status)
    const pages = await (await fetch('http://127.0.0.1:31000/json/list')).json()
    const page = pages.find(item => item.type === 'page' && item.webSocketDebuggerUrl)
    if (!page) throw Error('CDP page unavailable')
    browser = new WebSocket(page.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => { browser.onopen = resolve; browser.onerror = reject })
    const evaluate = expression => new Promise((resolve, reject) => {
      browser.onmessage = event => {
        const message = JSON.parse(event.data)
        if (message.id !== 1) return
        message.error ? reject(Error(message.error.message)) : resolve(message.result?.result?.value)
      }
      browser.send(JSON.stringify({id: 1, method: 'Runtime.evaluate', params: {expression, returnByValue: true}}))
    })
    await sleep(1000)
    const result = await evaluate(`(() => {
      const root = app.scene, stack = [root], types = {}, materialTypes = {}, geometryNames = {};
      let nodes = 0, meshes = 0, triangleEstimate = 0, geometryCount = 0;
      const large = [];
      while (stack.length) {
        const node = stack.pop(); if (!node || ++nodes > 50000) break;
        const kind = node.type || node.constructor?.name || 'unknown'; types[kind] = (types[kind] || 0) + 1;
        if (node.geometry) {
          meshes++; const g = node.geometry; geometryCount++;
          const tri = g.index ? g.index.count / 3 : (g.attributes?.position?.count || 0) / 3;
          triangleEstimate += tri;
          if (tri > 10000) large.push({name: node.name?.slice(0, 64), type: kind, triangles: Math.round(tri), visible: node.visible, material: node.material?.type, frustumCulled: node.frustumCulled});
          const name = g.type || g.constructor?.name || 'unknown'; geometryNames[name] = (geometryNames[name] || 0) + 1;
          const mat = node.material?.type || node.material?.constructor?.name || 'unknown'; materialTypes[mat] = (materialTypes[mat] || 0) + 1;
        }
        if (Array.isArray(node.children)) stack.push(...node.children);
      }
      large.sort((a,b) => b.triangles - a.triangles);
      return JSON.stringify({frame: app._currentFrame, renderStates: app.renderStates, rendererInfo: {memory: app.renderer.info.memory, render: app.renderer.info.render}, threeGlobal: typeof window.THREE, threeBasic: typeof window.THREE?.MeshBasicMaterial, rootType: root?.type, nodes, meshes, triangleEstimate: Math.round(triangleEstimate), types: Object.entries(types).sort((a,b)=>b[1]-a[1]).slice(0,20), materials: Object.entries(materialTypes).sort((a,b)=>b[1]-a[1]).slice(0,20), geometries: Object.entries(geometryNames).sort((a,b)=>b[1]-a[1]).slice(0,20), large: large.slice(0,8)});
    })()`)
    console.log(result)
  } finally {
    browser?.close()
    await sleep(200)
    await fetch(base + '/api/v1/admin/sessions/' + session.token + '/stop', {method: 'POST', headers, signal: AbortSignal.timeout(20000)})
  }
}
main().catch(error => { console.error(error); process.exitCode = 1 })
