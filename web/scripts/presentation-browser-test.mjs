import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve, extname, sep } from 'node:path'

const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../dist', import.meta.url))
const output = fileURLToPath(new URL('../../output/playwright/', import.meta.url))
let project = { token: 'presentation-fixture', name: 'FPS 验证', target_url: 'https://example.com', resolution: '1280x720', fps: 30,
  render_mode: 'cpu', concurrency_mode: 'single', max_viewers: 1, preload_count: 1, viewers: 0, running_instances: 1, running: true }
let failToggle = false, created = false
const writes = []
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path.startsWith('/api/')) {
    let data, status = 200
    if (req.method === 'POST' || req.method === 'PUT' || req.method === 'DELETE') {
      let body = ''; for await (const chunk of req) body += chunk
      const input = JSON.parse(body || '{}'); writes.push({ path, input })
      if (path.endsWith('/presentation') && failToggle) { status = 503; data = { msg: 'FPS 设置保存失败，请重试。' } }
      else {
        if (path === '/api/v1/sessions') created = true
        project = { ...project, ...input }
        data = { data: { token: project.token, show_fps: project.show_fps } }
      }
    } else if (path.endsWith('/auth/me')) data = { username: 'FPS 验证' }
    else if (path.endsWith('/admin/appearance')) data = { data: { theme: 'blue-tech' } }
    else if (path.endsWith('/admin/sessions')) data = { data: created ? [project] : [] }
    else if (path.endsWith('/admin/resources')) data = { data: { channel_capacity: 6, transport: 'jpeg', cpu_max_percent: 85 } }
    else if (path.startsWith('/api/v1/sessions/')) data = { data: { resolution: '1280x720', fps: 30, running: true, available_instances: 1, show_fps: project.show_fps, transport: path.endsWith('/rtc') ? 'webrtc' : 'jpeg' } }
    else data = { data: { enabled: false, issues: [], backups: [] } }
    res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); return
  }
  const file = path.startsWith('/assets/') ? resolve(root, '.' + path) : resolve(root, 'index.html')
  if (!file.startsWith(root + sep)) { res.writeHead(404); res.end(); return }
  try {
    res.writeHead(200, { 'Content-Type': ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[extname(file)] || 'application/octet-stream' })
    res.end(await readFile(file))
  } catch { res.writeHead(404); res.end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
let browser
try {
  await mkdir(output, { recursive: true })
  browser = await chromium.launch({ channel: 'msedge', headless: true })
  const context = await browser.newContext({ viewport: { width: 1440, height: 1040 } })
  const errors = []
  context.on('page', page => page.on('pageerror', cause => errors.push(cause.message)))
  await context.addInitScript(() => {
    // Exercise viewer UI and settings without starting browsers/renderers on the real service.
    window.__sockets = []
    window.WebSocket = class {
      static OPEN = 1
      readyState = 1
      constructor(url) {
        this.url = url; window.__sockets.push(this)
        setTimeout(() => {
          this.onopen?.({})
          if (this.binaryType === 'blob') {
            const canvas = document.createElement('canvas'); canvas.width = 1280; canvas.height = 720
            const ctx = canvas.getContext('2d'); ctx.fillStyle = '#163a55'; ctx.fillRect(0, 0, 1280, 720)
            canvas.toBlob(blob => this.onmessage?.({ data: blob }), 'image/jpeg')
          }
        }, 10)
      }
      send() {}
      close() { this.readyState = 3 }
    }
    try { localStorage.setItem('gostream3d.viewer.showFPS', '0') } catch { /* about:blank has no storage origin. */ }
  })
  const base = `http://127.0.0.1:${server.address().port}`
  const admin = await context.newPage()
  await admin.goto(base + '/admin#streams')
  await admin.locator('#show-fps').waitFor()
  assert.equal(await admin.locator('#show-fps').inputValue(), 'on', 'legacy default hidden')
  await admin.locator('#show-fps').selectOption('off')
  await admin.locator('#name').fill('FPS 验证')
  await admin.locator('#target-url').fill('https://example.com')
  await admin.getByRole('button', { name: '预加载并生成链接', exact: true }).click()
  await admin.waitForURL('**/admin#links')
  assert.equal(writes[0].input.show_fps, false)
  await admin.getByRole('button', { name: '编辑 FPS 验证', exact: true }).click()
  await admin.getByRole('dialog').waitFor()
  assert.equal(await admin.locator('#show-fps').inputValue(), 'off')
  await admin.locator('#show-fps').selectOption('on')
  await admin.getByRole('button', { name: '保存修改', exact: true }).click()
  await admin.getByRole('dialog').waitFor({ state: 'detached' })
  assert.equal(writes[1].input.show_fps, true)
  await admin.reload()
  await admin.getByRole('button', { name: '编辑 FPS 验证', exact: true }).click()
  await admin.getByRole('dialog').waitFor()
  assert.equal(await admin.locator('#show-fps').inputValue(), 'on', 'saved setting lost on reload')
  await admin.getByRole('button', { name: '取消', exact: true }).click()
  project.viewers = 4
  await admin.getByRole('button', { name: '刷新状态', exact: true }).click()
  await admin.waitForFunction(() => document.querySelector('button[aria-label="编辑 FPS 验证"]')?.disabled)
  const viewers = []
  for (const token of ['presentation-fixture', 'rtc']) {
    const page = await context.newPage()
    await page.goto(base + '/Stream3D/' + token)
    await page.locator('.viewer-fps').waitFor()
    viewers.push(page)
    assert.equal(await page.evaluate(() => window.__sockets.length), 1)
  }
  const menu = async () => {
    await admin.bringToFront()
    // A disappearing two-second notice can trigger a layout scroll, which closes menus.
    await admin.locator('.alert-success').waitFor({ state: 'detached' })
    const details = admin.locator('.project-more')
    const summary = admin.getByLabel('FPS 验证 的更多操作', { exact: true })
    await summary.scrollIntoViewIfNeeded()
    await admin.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    if (await details.getAttribute('open') === null) await summary.click()
    await admin.locator('.project-more[open] .dropdown-menu').waitFor()
  }
  await menu()
  assert.ok(await admin.getByRole('button', { name: '关闭 FPS 显示', exact: true }).isEnabled(), 'live viewing blocked FPS control')
  await admin.getByRole('button', { name: '关闭 FPS 显示', exact: true }).click()
  await admin.getByText('已关闭观看页 FPS 显示。', { exact: true }).waitFor()
  assert.equal(writes.at(-1).path, '/api/v1/admin/sessions/presentation-fixture/presentation')
  assert.deepEqual(writes.at(-1).input, { show_fps: false })
  for (const page of viewers) {
    await page.bringToFront()
    await page.locator('.viewer-fps').waitFor({ state: 'detached', timeout: 8000 })
    await page.mouse.move(1420, 1020)
    assert.equal(await page.locator('.viewer-fps, .viewer-fps-toggle').count(), 0, 'hover reveals hidden FPS')
    assert.equal(await page.evaluate(() => window.__sockets.length), 1, 'presentation reconnected stream')
    await page.reload()
    await page.locator('.viewer-stage').waitFor()
    assert.equal(await page.locator('.viewer-fps').count(), 0, 'hidden setting lost on reload')
  }
  failToggle = true
  await menu()
  await admin.getByRole('button', { name: '开启 FPS 显示', exact: true }).click()
  await admin.getByText('FPS 设置保存失败，请重试。', { exact: true }).waitFor()
  assert.equal(project.show_fps, false, 'failed request changed presentation')
  await menu()
  assert.ok(await admin.getByRole('button', { name: '开启 FPS 显示', exact: true }).isEnabled())
  failToggle = false
  await admin.getByRole('button', { name: '开启 FPS 显示', exact: true }).click()
  await admin.getByText('已开启观看页 FPS 显示。', { exact: true }).waitFor()
  for (const page of viewers) {
    await page.bringToFront()
    await page.locator('.viewer-fps').waitFor({ timeout: 8000 })
    assert.equal(await page.evaluate(() => window.__sockets.length), 1)
  }
  await admin.bringToFront()
  await admin.setViewportSize({ width: 390, height: 844 })
  await admin.getByRole('button', { name: '卡片视图', exact: true }).click()
  await menu()
  const bounds = await admin.locator('.project-more[open] .dropdown-menu').boundingBox()
  assert.ok(bounds.y >= 0 && bounds.y + bounds.height <= 844, 'expanded menu overflows mobile viewport')
  await admin.screenshot({ path: resolve(output, 'project-fps-menu-mobile.png'), fullPage: true })
  assert.deepEqual(errors, [])
  console.log('FPS create/edit persistence, live-viewer menu toggle, JPEG/WebRTC visibility, hidden DOM, reload, failure/retry and mobile menu passed; only fixture data was used.')
} finally {
  await browser?.close()
  await new Promise(resolve => server.close(resolve))
}
