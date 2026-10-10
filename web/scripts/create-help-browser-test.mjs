import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve, extname, sep } from 'node:path'

const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../dist', import.meta.url))
const output = fileURLToPath(new URL('../../output/playwright/', import.meta.url))
const writes = []
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path.startsWith('/api/')) {
    let data
    if (req.method === 'POST' || req.method === 'PUT') {
      let body = ''; for await (const chunk of req) body += chunk
      writes.push({ path, body: JSON.parse(body || '{}') }); data = { data: { token: 'help-fixture' } }
    } else if (path.endsWith('/auth/me')) data = { username: '帮助测试' }
    else if (path.endsWith('/admin/appearance')) data = { data: { theme: 'blue-tech' } }
    else if (path.endsWith('/admin/sessions')) data = { data: [] }
    else if (path.endsWith('/admin/resources')) data = { data: { channel_capacity: 3, transport: 'jpeg', cpu_max_percent: 50, disk_cache_enabled: true } }
    else data = { data: { enabled: false, issues: [], backups: [] } }
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); return
  }
  const file = path.startsWith('/assets/') ? resolve(root, '.' + path) : resolve(root, 'index.html')
  if (!file.startsWith(root + sep)) { res.writeHead(404); res.end(); return }
  try {
    const body = await readFile(file)
    res.writeHead(200, { 'Content-Type': ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[extname(file)] || 'application/octet-stream' }); res.end(body)
  } catch { res.writeHead(404); res.end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
let browser
try {
  await mkdir(output, { recursive: true })
  browser = await chromium.launch({ channel: 'msedge', headless: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  const errors = []; page.on('pageerror', error => errors.push(error.message))
  const url = `http://127.0.0.1:${server.address().port}/admin#streams`
  await page.goto(url)
  const tip = page.getByRole('tooltip')
  const help = label => page.getByRole('button', { name: `${label}说明`, exact: true })
  await help('创建观看链接').hover()
  await tip.waitFor()
  assert.match(await tip.innerText(), /配置 3D 页面与渲染参数/)
  await tip.hover()
  await page.waitForTimeout(200)
  assert.ok(await tip.isVisible(), 'hovering the explanation must keep it readable')
  await page.keyboard.press('Escape'); await tip.waitFor({ state: 'detached' })
  await help('创建观看链接').focus()
  await tip.waitFor()
  assert.equal(await help('创建观看链接').getAttribute('aria-describedby'), await tip.getAttribute('id'))
  await page.keyboard.press('Escape'); await tip.waitFor({ state: 'detached' })
  await page.getByRole('heading', { name: '创建观看链接', exact: true }).click()
  await help('创建观看链接').click(); await tip.waitFor()
  await page.getByRole('heading', { name: '创建观看链接', exact: true }).click()
  await tip.waitFor({ state: 'detached' })
  assert.equal(writes.length, 0, 'help must never submit the project')
  for (const [label, explanation] of [
    ['项目与页面', /填写项目名称/], ['渲染配置', /CPU 模式保持所选分辨率/], ['观看模式', /操作互不干扰/],
    ['分辨率', /自适应/], ['目标帧率', /90\/120 FPS/],
    ['优先保留预加载路数', /可选 0–1 路/], ['观看页 FPS 显示', /不改变目标帧率/],
  ]) {
    await help(label).hover(); await tip.waitFor()
    assert.match(await tip.innerText(), explanation)
    await page.keyboard.press('Escape'); await tip.waitFor({ state: 'detached' })
  }
  assert.equal(await page.locator('.configuration-panel .field-hint,.configuration-panel .inline-info,.config-step-heading p').count(), 0)
  assert.equal(await page.locator('#resolution').inputValue(), '1920x1080')
  await page.locator('#resolution').selectOption('2560x1440')
  assert.equal(await page.locator('#fps').inputValue(), '30')
  assert.ok(await page.locator('#fps option[value="60"]').isDisabled())
  await page.locator('#resolution').selectOption('1920x1080')
  await page.screenshot({ path: resolve(output, 'create-help-desktop.png'), fullPage: true })
  await help('优先保留预加载路数').hover(); await tip.waitFor()
  await page.screenshot({ path: resolve(output, 'create-help-tooltip-desktop.png'), fullPage: true })
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /多路独立/ }).click()
  await page.locator('#max-viewers').fill('10')
  await page.locator('#preload-count').fill('6')
  await help('预加载路数').hover(); await tip.waitFor()
  assert.match(await tip.innerText(), /1–10 路/)
  assert.match(await tip.innerText(), /最多提前准备 6 路/)
  await page.keyboard.press('Escape')
  await help('优先保留预加载路数').hover(); await tip.waitFor()
  assert.match(await tip.innerText(), /可选 0–6 路/)
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /本机 GPU/ }).click()
  await help('GPU 画面策略').hover(); await tip.waitFor()
  assert.match(await tip.innerText(), /无人操作时降低帧率/)
  await page.keyboard.press('Escape')
  await help('分辨率').click(); await tip.waitFor()
  await help('目标帧率').hover()
  await tip.waitFor()
  assert.equal(await tip.count(), 1, 'moving between question marks must not leave overlapping popups')
  assert.match(await tip.innerText(), /目标帧率/)
  await page.keyboard.press('Escape')
  await page.locator('#target-url').fill('https://example.com/digital-twin')
  await page.locator('#reserved-preloads').selectOption('2')
  await page.locator('#show-fps').selectOption('off')
  await page.getByRole('button', { name: '预加载并生成链接', exact: true }).click()
  await page.waitForURL('**/admin#links')
  assert.equal(writes.length, 1)
  assert.equal(writes[0].body.max_viewers, 10)
  assert.equal(writes[0].body.preload_count, 6)
  assert.equal(writes[0].body.reserved_preloads, 2)
  assert.equal(writes[0].body.show_fps, false)
  assert.equal(writes[0].body.render_mode, 'nvidia')

  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  mobile.on('pageerror', error => errors.push(error.message))
  await mobile.goto(url)
  await mobile.getByRole('button', { name: '创建观看链接说明', exact: true }).tap()
  await mobile.getByRole('tooltip').waitFor()
  const box = await mobile.getByRole('tooltip').boundingBox()
  assert.ok(box.x >= 0 && box.x + box.width <= 390 && box.y >= 0 && box.y + box.height <= 844, 'mobile tooltip must fit the viewport')
  await mobile.screenshot({ path: resolve(output, 'create-help-mobile.png') })
  await mobile.getByRole('heading', { name: '创建观看链接', exact: true }).tap()
  await mobile.getByRole('tooltip').waitFor({ state: 'detached' })
  assert.ok(await mobile.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page overflow')
  assert.deepEqual(errors, [])
  console.log('Creation help browser checks passed: hover/focus/Escape/outside click, mobile tap/position, parameter descriptions and dynamic limits, no accidental submission and unchanged creation payload.')
} finally {
  await browser?.close()
  await new Promise(resolve => server.close(resolve))
}
