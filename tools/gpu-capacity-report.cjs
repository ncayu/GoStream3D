// Measure 1–3 simultaneous, independent viewers of the paused campus GPU project.
// The selected project is restarted for this diagnostic and restored to paused.
const fs = require('node:fs')
const path = require('node:path')
const {execFile, execFileSync} = require('node:child_process')
const {promisify} = require('node:util')
const runFile = promisify(execFile)
const base = process.env.GOSTREAM3D_PROBE_BASE_URL || 'http://127.0.0.1:8888'
const expectedName = process.env.GOSTREAM3D_PROBE_PROJECT_NAME
if (!expectedName) throw Error('Set GOSTREAM3D_PROBE_PROJECT_NAME before restarting a project')
const token = process.env.GOSTREAM3D_PROBE_TOKEN
if (!token) throw Error('Set GOSTREAM3D_PROBE_TOKEN to the selected project token')
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))

const env = Object.fromEntries(fs.readFileSync(path.join(__dirname, '..', 'dist', 'package-windows-amd64', 'config.env'), 'utf8')
  .split(/\r?\n/).filter(line => /^GOSTREAM3D_\w+=/.test(line))
  .map(line => { const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1).trim().replace(/^(['"])(.*)\1$/, '$2')] }))

function gpuSample() {
  try {
    const raw = execFileSync('nvidia-smi', ['--query-gpu=memory.total,memory.used,utilization.gpu,utilization.encoder', '--format=csv,noheader,nounits'], {encoding: 'utf8', timeout: 3000}).trim()
    const [totalMiB, usedMiB, gpuPercent, encoderPercent] = raw.split(',').map(value => Number(value.trim()))
    return {totalMiB, usedMiB, gpuPercent, encoderPercent}
  } catch { return null }
}

async function main() {
  const login = await fetch(base + '/api/v1/auth/login', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({username: env.GOSTREAM3D_ADMIN_USERNAME, password: env.GOSTREAM3D_ADMIN_PASSWORD})})
  if (!login.ok) throw Error('admin login failed')
  const headers = {Cookie: login.headers.getSetCookie().map(cookie => cookie.split(';')[0]).join('; ')}
  const request = async (url, options = {}) => {
    const response = await fetch(base + url, {...options, headers, signal: AbortSignal.timeout(30000)})
    if (!response.ok) throw Error(url + ': HTTP ' + response.status + ' ' + await response.text())
    return (await response.json()).data
  }
  const getTarget = async () => (await request('/api/v1/admin/sessions')).find(session => session.token === token)
  const before = await getTarget()
  const channels = await request('/api/v1/admin/channels/settings')
  if (!before || before.name !== expectedName || before.render_mode !== 'nvidia' || !before.render_paused || before.viewers !== 0) throw Error('target GPU project must be paused and have no viewers')
  if (before.preload_count !== 3 || channels.max_channels < 3 || channels.active_channels !== 0) throw Error('expected three preloads and three free channels')
  const report = {project: '示例项目 A', resolution: before.resolution, target_fps: before.fps, configured_channels: channels.max_channels, project_viewer_quota: before.max_viewers, baseline_gpu: gpuSample(), runs: []}
  let started = false
  try {
    await request('/api/v1/admin/sessions/' + token + '/start?async=1', {method: 'POST'})
    started = true
    const deadline = Date.now() + 240000
    let ready
    while (Date.now() < deadline) {
      ready = await getTarget()
      if (ready.ready_instances === 3 && !ready.starting_instances) break
      if (ready.preload_error || ready.pool_error) throw Error(ready.preload_error || ready.pool_error)
      await sleep(1500)
    }
    if (ready?.ready_instances !== 3) throw Error('three preloaded GPU instances were not ready')
    // Ready means the scene is visible; WebGL shaders and textures can still
    // be settling. Measure steady interaction after a short warm-up.
    await sleep(10000)
    report.preloaded_gpu = gpuSample()
    for (const count of [1, 2, 3]) {
      const samples = []
      const interval = setInterval(() => { const sample = gpuSample(); if (sample) samples.push(sample) }, 1500)
      try {
        const result = await runFile(process.execPath, [path.join(__dirname, 'three-stream-fps-probe.cjs'), '12000', String(count), 'all-active'], {cwd: path.join(__dirname, '..'), timeout: 45000})
        report.runs.push({viewers: count, result: JSON.parse(result.stdout.trim()), gpu_samples: samples})
        console.log(JSON.stringify({viewers: count, streams: report.runs.at(-1).result.streams, gpu_peak: Math.max(...samples.map(x => x.gpuPercent)), vram_peak_mib: Math.max(...samples.map(x => x.usedMiB))}))
      } finally { clearInterval(interval) }
      await sleep(2500)
    }
  } finally {
    if (started) {
      await request('/api/v1/admin/sessions/' + token + '/stop', {method: 'POST'})
      const after = await getTarget()
      report.restored_paused = after.render_paused && after.running_instances === 0 && after.viewers === 0
    }
  }
  fs.mkdirSync(path.join(__dirname, '..', '.cache'), {recursive: true})
  fs.writeFileSync(path.join(__dirname, '..', '.cache', 'gpu-capacity-report.json'), JSON.stringify(report, null, 2))
  console.log(JSON.stringify({restored_paused: report.restored_paused, baseline_gpu: report.baseline_gpu, preloaded_gpu: report.preloaded_gpu}))
}

main().catch(error => { console.error(error); process.exitCode = 1 })
