import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve, extname, sep } from 'node:path'

const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../dist', import.meta.url))
const output = fileURLToPath(new URL('../../output/playwright/', import.meta.url))
let capacity = 6, resourcesAvailable = true
const writes = []
const project = { token: 'limits-fixture', name: '路数验证', target_url: 'https://example.com', resolution: '1920x1080',
  fps: 30, render_mode: 'nvidia', concurrency_mode: 'multi', max_viewers: 10, preload_count: 8, reserved_preloads: 7,
  viewers: 0, running_instances: 0, render_paused: true }
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path.startsWith('/api/')) {
    let data, status = 200
    if (req.method === 'POST' || req.method === 'PUT' || req.method === 'DELETE') {
      let body = ''; for await (const chunk of req) body += chunk
      writes.push({ path, body: JSON.parse(body || '{}') })
      data = { data: { token: project.token } }
    } else if (path.endsWith('/auth/me')) data = { username: '路数验证' }
    else if (path.endsWith('/admin/appearance')) data = { data: { theme: 'default' } }
    else if (path.endsWith('/admin/sessions')) data = { data: [project] }
    else if (path.endsWith('/admin/resources')) {
      status = resourcesAvailable ? 200 : 503
      data = resourcesAvailable ? { data: { channel_capacity: capacity, transport: 'jpeg', cpu_max_percent: 85 } } : { msg: 'fixture unavailable' }
    } else data = { data: { enabled: false, issues: [], backups: [] } }
    res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); return
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
  const page = await browser.newPage({ viewport: { width: 1440, height: 1040 } })
  const errors = []; page.on('pageerror', error => errors.push(error.message))
  const url = `http://127.0.0.1:${server.address().port}/admin`
  const reservationOptions = () => page.locator('#reserved-preloads option').evaluateAll(elements => elements.map(element => element.value))
  const assertValue = async (id, value) => assert.equal(await page.locator(`#${id}`).inputValue(), String(value))
  await page.goto(url)
  await page.getByRole('button', { name: '创建新链接', exact: true }).click()
  await page.getByRole('button', { name: '预加载并生成链接', exact: true }).waitFor()
  await page.waitForFunction(() => !document.querySelector('button[type=submit]').disabled)
  assert.deepEqual(await reservationOptions(), ['0', '1'])
  await page.locator('#reserved-preloads').selectOption('1')
  await page.getByRole('button', { name: /多路独立/ }).click()
  await assertValue('max-viewers', 3); await assertValue('preload-count', 3)
  assert.equal(await page.locator('#max-viewers').getAttribute('max'), '100')
  await page.locator('#max-viewers').fill('101'); await assertValue('max-viewers', 100)
  await page.locator('#max-viewers').fill('10'); await assertValue('max-viewers', 10)
  await page.locator('#preload-count').fill('99'); await assertValue('preload-count', 10)
  await page.locator('#reserved-preloads').selectOption('10')
  await page.locator('#max-viewers').fill('2')
  await assertValue('max-viewers', 2); await assertValue('preload-count', 2); await assertValue('reserved-preloads', 2)
  assert.deepEqual(await reservationOptions(), ['0', '1', '2'])
  await page.locator('#preload-count').fill('-5'); await assertValue('preload-count', 1); await assertValue('reserved-preloads', 1)
  await page.getByRole('button', { name: /单路会话/ }).click()
  assert.deepEqual(await reservationOptions(), ['0', '1'])
  await page.locator('#target-url').fill('https://example.com')
  await page.getByRole('button', { name: '预加载并生成链接', exact: true }).click()
  await page.waitForURL('**/admin#links')
  assert.equal(writes[0].body.max_viewers, 1); assert.equal(writes[0].body.preload_count, 1); assert.equal(writes[0].body.reserved_preloads, 1)
  await page.getByRole('button', { name: '编辑 路数验证', exact: true }).click()
  await page.getByRole('dialog').waitFor()
  await assertValue('edit-viewers', 10); await assertValue('edit-preload', 8); await assertValue('reserved-preloads', 7)
  await page.locator('#edit-viewers').fill('3')
  await assertValue('edit-preload', 3); await assertValue('reserved-preloads', 3)
  await page.locator('#edit-preload').fill('2'); await assertValue('reserved-preloads', 2)
  await page.locator('#edit-viewers').fill('8')
  await page.locator('#edit-preload').fill('99'); await assertValue('edit-preload', 8)
  await page.locator('#reserved-preloads').selectOption('8')
  await page.getByRole('button', { name: '保存修改', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'detached' })
  assert.equal(writes[1].body.max_viewers, 8); assert.equal(writes[1].body.preload_count, 8); assert.equal(writes[1].body.reserved_preloads, 8)
  await page.getByRole('button', { name: '编辑 路数验证', exact: true }).click()
  await page.getByRole('dialog').waitFor()
  await page.getByRole('dialog').getByRole('button', { name: '单路会话', exact: true }).click()
  assert.deepEqual(await reservationOptions(), ['0', '1'])
  await page.getByRole('button', { name: '保存修改', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'detached' })
  assert.equal(writes[2].body.max_viewers, 1); assert.equal(writes[2].body.preload_count, 1); assert.equal(writes[2].body.reserved_preloads, 1)
  capacity = 2
  await page.goto(url)
  await page.getByRole('button', { name: '创建新链接', exact: true }).click()
  await page.getByRole('button', { name: /多路独立/ }).click()
  await assertValue('max-viewers', 3); await assertValue('preload-count', 3)
  await page.locator('#max-viewers').fill('10')
  await page.locator('#preload-count').fill('8')
  await page.locator('#reserved-preloads').selectOption('7')
  capacity = 1
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await page.getByText('服务器共 1 路', { exact: false }).waitFor()
  assert.ok(await page.getByRole('button', { name: /多路独立/ }).isEnabled())
  await assertValue('max-viewers', 10); await assertValue('preload-count', 8); await assertValue('reserved-preloads', 7)
  await page.screenshot({ path: resolve(output, 'session-limits-independent-quota.png'), fullPage: true })
  await page.locator('#target-url').fill('https://example.com')
  await page.getByRole('button', { name: '预加载并生成链接', exact: true }).click()
  await page.waitForURL('**/admin#links')
  assert.equal(writes[3].body.max_viewers, 10); assert.equal(writes[3].body.preload_count, 8); assert.equal(writes[3].body.reserved_preloads, 7)
  resourcesAvailable = false
  await page.goto(url)
  await page.getByRole('button', { name: '创建新链接', exact: true }).click()
  await page.getByText('fixture unavailable', { exact: false }).waitFor()
  await page.getByRole('button', { name: /多路独立/ }).click()
  await assertValue('max-viewers', 3); await assertValue('preload-count', 3)
  assert.ok(await page.getByRole('button', { name: '预加载并生成链接', exact: true }).isEnabled())
  assert.deepEqual(errors, [])
  console.log('Browser single 0/1, independent project quotas, preload/reservation bounds, create/edit requests, unchanged quotas after capacity reduction and unavailable capacity passed; only fixture data was used.')
} finally {
  await browser?.close()
  await new Promise(resolve => server.close(resolve))
}
