import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve, extname, sep } from 'node:path'

// Optional real-browser check; use installed Playwright or the bundled runtime.
const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../dist', import.meta.url))
const output = fileURLToPath(new URL('../../output/playwright/', import.meta.url))
let theme = 'emerald', writes = 0, failSave = false, failRead = false
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path.startsWith('/api/')) {
    let status = 200, data
    if (path.endsWith('/auth/me')) data = { username: '界面测试' }
    else if (path.endsWith('/admin/appearance')) {
      if (req.method === 'PUT') {
        writes++
        let body = ''; for await (const chunk of req) body += chunk
        if (failSave) { status = 500; data = { msg: '数据库暂时无法写入，请重试。' } }
        else { theme = JSON.parse(body).theme; data = { code: 0, data: { theme } } }
      } else if (failRead) { status = 500; data = { msg: '无法读取已保存的配色，请重试。' } }
      else data = { code: 0, data: { theme } }
    } else if (path.endsWith('/admin/sessions')) data = { data: [] }
    else if (path.endsWith('/admin/resources')) data = { data: { cpu_max_percent: 50, channel_capacity: 3 } }
    else data = { data: { enabled: false, issues: [], backups: [] } }
    res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); return
  }
  const file = path.startsWith('/assets/') ? resolve(root, '.' + path) : resolve(root, 'index.html')
  if (!file.startsWith(root + sep)) { res.writeHead(404); res.end(); return }
  try { res.writeHead(200, { 'Content-Type': ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[extname(file)] || 'application/octet-stream' }); res.end(await readFile(file)) }
  catch { res.end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
let browser
try {
  browser = await chromium.launch({ channel: 'msedge', headless: true })
  const context = await browser.newContext({ viewport: { width: 1440, height: 960 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  const url = `http://127.0.0.1:${server.address().port}/admin#settings`
  const saved = () => page.locator('.theme-save-bar button')
  const selected = () => page.locator('.console-theme-card[aria-pressed="true"]')
  const ready = () => page.waitForFunction(() => { const button = document.querySelector('.theme-save-bar button'); return button && !button.disabled })
  await page.goto(url)
  await ready()
  assert.match(await selected().innerText(), /绿色/)
  await page.getByRole('button', { name: /^紫色/ }).click()
  assert.equal(theme, 'emerald'); assert.equal(writes, 0)
  assert.match(await page.locator('.theme-save-bar').innerText(), /尚未保存/)
  await saved().click()
  const dialog = page.getByRole('alertdialog')
  await dialog.waitFor()
  assert.match(await dialog.innerText(), /配色保存成功/)
  assert.equal(theme, 'aurora')
  await page.screenshot({ path: resolve(output, 'theme-save-success-desktop.png'), fullPage: true })
  await page.keyboard.press('Escape')
  assert.equal(await dialog.count(), 0)
  assert.ok(await saved().evaluate(element => element === document.activeElement), 'dialog did not restore button focus')
  await page.reload(); await ready()
  assert.match(await selected().innerText(), /紫色/)
  await page.getByRole('button', { name: /^橙色/ }).click()
  await page.reload(); await ready()
  assert.match(await selected().innerText(), /紫色/, 'unsaved preview persisted')
  failSave = true
  await page.getByRole('button', { name: /^玫红/ }).click()
  await saved().click(); await dialog.waitFor()
  assert.match(await dialog.innerText(), /配色保存失败/)
  assert.equal(theme, 'aurora')
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  failSave = false
  await saved().click(); await dialog.waitFor()
  assert.match(await dialog.innerText(), /配色保存成功/)
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  failRead = true
  await page.reload()
  await page.getByRole('button', { name: '重新加载配色' }).waitFor()
  assert.ok(await saved().isDisabled())
  failRead = false
  await page.getByRole('button', { name: '重新加载配色' }).click(); await ready()
  assert.match(await selected().innerText(), /玫红/)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: resolve(output, 'theme-settings-mobile.png'), fullPage: true })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile overflow')
  await page.getByRole('button', { name: '恢复默认配色' }).click()
  assert.equal(theme, 'rose', 'reset saved without clicking save')
  await saved().click(); await dialog.waitFor()
  assert.equal(theme, 'blue-tech')
  await page.screenshot({ path: resolve(output, 'theme-save-success-mobile.png'), fullPage: true })
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await context.clearCookies()
  await page.reload(); await ready()
  assert.match(await selected().innerText(), /蓝色/)
  assert.deepEqual(errors, [])
  console.log('Browser theme preview/save/reload, failure/retry, read protection, modal focus and mobile layout passed.')
} finally {
  await browser?.close()
  await new Promise(resolve => server.close(resolve))
}
