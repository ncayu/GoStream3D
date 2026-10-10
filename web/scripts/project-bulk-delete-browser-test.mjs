import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve, extname, sep } from 'node:path'

const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../dist', import.meta.url))
const output = fileURLToPath(new URL('../../output/playwright/', import.meta.url))
const initial = Array.from({ length: 12 }, (_, index) => ({
  token: `test-${index + 1}`, name: `项目${String(index + 1).padStart(2, '0')}`, target_url: `https://example.com/${index + 1}`,
  resolution: '1920x1080', fps: 30, render_mode: index % 2 ? 'nvidia' : 'cpu', concurrency_mode: 'multi',
  max_viewers: 3, preload_count: 1, running_instances: index ? 0 : 1, ready_instances: index ? 0 : 1,
  running: index === 0, viewers: index ? 0 : 2, render_paused: index !== 0,
}))
let projects = [...initial], failToken = 'test-2'
const deletions = []
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path.startsWith('/api/')) {
    let status = 200, data
    if (path.endsWith('/auth/me')) data = { username: '批量删除验证' }
    else if (path.endsWith('/admin/appearance')) data = { code: 0, data: { theme: 'glacier' } }
    else if (path.endsWith('/admin/sessions')) data = { data: projects }
    else if (path.includes('/admin/sessions/') && req.method === 'DELETE') {
      const token = decodeURIComponent(path.split('/').at(-1)); deletions.push(token)
      await new Promise(resolve => setTimeout(resolve, 180))
      if (token === failToken) { status = 503; data = { msg: '实例回收失败，请重试。' } }
      else if (!projects.some(item => item.token === token)) { status = 404; data = { msg: 'session not found' } }
      else { projects = projects.filter(item => item.token !== token); status = 204 }
    } else if (path.endsWith('/admin/resources')) data = { data: { channel_capacity: 6, cpu_max_percent: 85 } }
    else data = { data: { enabled: false, issues: [], backups: [] } }
    res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(status === 204 ? undefined : JSON.stringify(data)); return
  }
  const file = path.startsWith('/assets/') ? resolve(root, '.' + path) : resolve(root, 'index.html')
  if (!file.startsWith(root + sep)) { res.writeHead(404); res.end(); return }
  try { const body = await readFile(file); res.writeHead(200, { 'Content-Type': ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[extname(file)] || 'application/octet-stream' }); res.end(body) }
  catch { res.writeHead(404); res.end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
let browser
try {
  browser = await chromium.launch({ channel: 'msedge', headless: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1040 } })
  const errors = []
  page.on('pageerror', cause => errors.push(cause.message))
  await page.goto(`http://127.0.0.1:${server.address().port}/admin#links`)
  await page.getByRole('checkbox', { name: '选择 项目01', exact: true }).waitFor()
  const bulk = () => page.getByRole('button', { name: /^批量删除/ })
  const select = name => page.getByRole('checkbox', { name: `选择 ${name}`, exact: true })
  const selectPage = () => page.getByRole('checkbox', { name: '全选当前页项目', exact: true })
  const dialog = () => page.getByRole('alertdialog')
  const selection = () => page.locator('.project-bulk-bar [role="status"]')
  assert.ok(await bulk().isDisabled())
  await select('项目01').check()
  assert.ok(await selectPage().evaluate(element => element.indeterminate), 'header lacks mixed selection')
  await selectPage().check()
  assert.equal(await page.locator('tbody input:checked').count(), 10)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  assert.equal(await select('项目11').isChecked(), false)
  await select('项目11').check()
  assert.match(await selection().innerText(), /已选 11.*其他页 10/)
  await page.getByRole('button', { name: '取消选择', exact: true }).click()
  assert.match(await selection().innerText(), /已选 0/)
  await select('项目12').check()
  await page.getByRole('searchbox', { name: '搜索项目名称或地址' }).fill('项目01')
  assert.match(await selection().innerText(), /已选 0/, 'filter retained hidden selection')
  await page.getByRole('searchbox', { name: '搜索项目名称或地址' }).fill('')
  await select('项目01').check(); await select('项目02').check()
  await page.getByRole('button', { name: '卡片视图', exact: true }).click()
  assert.ok(await select('项目01').isChecked())
  await bulk().click(); await dialog().waitFor()
  assert.match(await dialog().innerText(), /所选 2 个项目/)
  assert.match(await dialog().innerText(), /项目01/)
  assert.match(await dialog().innerText(), /项目02/)
  assert.match(await dialog().innerText(), /断开这 2 位观看者/)
  await page.keyboard.press('Escape')
  assert.equal(await dialog().count(), 0)
  assert.equal(deletions.length, 0, 'opening/cancelling confirmation deleted projects')
  await page.getByRole('button', { name: '列表视图', exact: true }).click()
  await page.screenshot({ path: resolve(output, 'project-bulk-selection-desktop.png'), fullPage: true })
  await bulk().click()
  await page.screenshot({ path: resolve(output, 'project-bulk-confirm-desktop.png'), fullPage: true })
  await dialog().getByRole('button', { name: '确认删除', exact: true }).click()
  assert.ok(await dialog().getByRole('button', { name: '取消', exact: true }).isDisabled())
  await page.keyboard.press('Escape')
  assert.equal(await dialog().count(), 1, 'active deletion dialog dismissed')
  await page.getByRole('button', { name: '重试未删除项目', exact: true }).waitFor()
  assert.match(await dialog().innerText(), /已删除 1 个项目，1 个未删除/)
  assert.equal(await select('项目01').count(), 0)
  assert.ok(await select('项目02').isChecked())
  assert.deepEqual(deletions, ['test-1', 'test-2'])
  failToken = ''
  await page.getByRole('button', { name: '重试未删除项目', exact: true }).click()
  await dialog().waitFor({ state: 'detached' })
  assert.deepEqual(deletions, ['test-1', 'test-2', 'test-2'], 'retry touched successfully removed projects')
  assert.match(await selection().innerText(), /已选 0/)
  assert.equal(projects.length, 10)
  assert.ok(await page.getByRole('searchbox', { name: '搜索项目名称或地址' }).evaluate(element => element === document.activeElement), 'deletion left focus on a removed element')
  await select('项目03').check()
  await bulk().click()
  projects = projects.filter(item => item.token !== 'test-3') // Another administrator removes it.
  await dialog().getByRole('button', { name: '确认删除', exact: true }).click()
  await dialog().waitFor({ state: 'detached' })
  assert.equal(await select('项目03').count(), 0)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: '卡片视图', exact: true }).click()
  await selectPage().check()
  assert.match(await selection().innerText(), /已选 9/)
  await page.screenshot({ path: resolve(output, 'project-bulk-selection-mobile.png'), fullPage: true })
  await bulk().click()
  await page.screenshot({ path: resolve(output, 'project-bulk-confirm-mobile.png'), fullPage: true })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page overflow')
  await dialog().getByRole('button', { name: '取消', exact: true }).click()
  const beforeSingle = deletions.length
  await page.getByLabel('项目04 的更多操作', { exact: true }).click()
  await page.getByRole('button', { name: '删除项目', exact: true }).click()
  assert.match(await dialog().innerText(), /删除项目？/)
  assert.match(await dialog().innerText(), /项目04/)
  await dialog().getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(deletions.length, beforeSingle, 'cancelling single deletion made a request')
  assert.deepEqual(errors, [])
  console.log('Browser project multi-select, page selection, filters, list/cards, confirmation, partial failure/retry, missing projects and mobile layout passed.')
} finally {
  await browser?.close()
  await new Promise(resolve => server.close(resolve))
}
