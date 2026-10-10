import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { mkdir, readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { resolve, extname, sep } from 'node:path'

const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../dist', import.meta.url))
const output = fileURLToPath(new URL('../../output/playwright/', import.meta.url))
const name = 'NVIDIA GeForce RTX 3050 Ti Laptop GPU'
const device = (id, utilization) => ({ id, name, utilization_percent: utilization })
const now = Date.now()
const history = Array.from({ length: 20 }, (_, index) => ({
  time: new Date(now - (19 - index) * 30_000).toISOString(),
  rx_mbps: 1, tx_mbps: 2, cpu_percent: 20, memory_percent: 35,
  gpus: [device('GPU-a', index === 8 ? null : 20 + index * 3), device('GPU-b', 65 - index * 2)],
}))
let snapshot = {
  sampled_at: new Date(now).toISOString(), interval_seconds: 30, cpu_percent: 20, cpu_cores: 8,
  memory: { used_percent: 35, used_bytes: 3 * 1024 ** 3, total_bytes: 8 * 1024 ** 3 },
  disk: { used_percent: 25, used_bytes: 25 * 1024 ** 3, total_bytes: 100 * 1024 ** 3 },
  partitions: [], interfaces: [], rx_mbps: 1, tx_mbps: 2, errors: [],
  system: { os: 'windows', platform: 'Windows', version: '11', kernel: '', hostname: 'GPU-test', uptime_seconds: 3600, local_ip: '127.0.0.1' },
  gpus: history.at(-1).gpus.map(gpu => ({ ...gpu, encoder_percent: 0, memory_used_bytes: 3.6 * 1024 ** 3, memory_total_bytes: 4 * 1024 ** 3 })),
  gpu_message: '', history,
}
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname
  if (path.startsWith('/api/')) {
    let data
    if (path.endsWith('/auth/me')) data = { username: '界面测试' }
    else if (path.endsWith('/admin/appearance')) data = { data: { theme: 'blue-tech' } }
    else if (path.endsWith('/admin/sessions')) data = { data: [] }
    else if (path.endsWith('/admin/resources')) data = { data: { channel_capacity: 3, cpu_max_percent: 50 } }
    else if (path.endsWith('/admin/performance')) data = { data: { settings: { gpu_fps_budget: 120, reclaim_idle_preloads: true }, streams: [], jpeg_metrics_supported: true } }
    else if (path.endsWith('/admin/monitor') || path.endsWith('/admin/monitor/refresh')) data = { data: snapshot }
    else data = { data: { enabled: false, issues: [], backups: [] } }
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); return
  }
  const file = path.startsWith('/assets/') ? resolve(root, '.' + path) : resolve(root, 'index.html')
  if (!file.startsWith(root + sep)) { res.writeHead(404); res.end(); return }
  try {
    const body = await readFile(file)
    res.writeHead(200, { 'Content-Type': ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' })[extname(file)] || 'application/octet-stream' })
    res.end(body)
  } catch { res.writeHead(404); res.end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
let browser
let page
const errors = []
try {
  await mkdir(output, { recursive: true })
  browser = await chromium.launch({ channel: 'msedge', headless: true })
  page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  page.on('pageerror', error => errors.push(error.stack || error.message))
  await page.goto(`http://127.0.0.1:${server.address().port}/admin#monitor`)
  const panel = page.locator('.gpu-monitor')
  const chart = panel.getByRole('img', { name: '近一小时GPU 使用率趋势图', exact: true })
  await chart.locator('svg').waitFor()
  await panel.scrollIntoViewIfNeeded()
  assert.match(await panel.innerText(), /近 1 小时 · 每 30 秒采样/)
  assert.equal(await panel.locator('.gpu-card').count(), 2)
  const graph = await chart.locator('svg').textContent()
  assert.match(graph, /100%/)
  assert.match(graph, /GPU 1/)
  assert.match(graph, /GPU 2/)
  const box = await chart.boundingBox()
  await page.mouse.move(box.x + box.width * .5, box.y + 70)
  await page.waitForFunction(() => [...document.querySelectorAll('.gpu-monitor div')].some(element => element.style.position === 'absolute' && element.textContent.includes('GPU 1') && element.textContent.includes('%')))
  await page.mouse.move(0, 0)
  await panel.screenshot({ path: resolve(output, 'gpu-utilization-trend-desktop.png') })
  for (const label of ['近一小时整机 CPU 使用率趋势图', '近一小时内存使用率趋势图', '近一小时网络接收和发送速率趋势图']) {
    assert.equal(await page.getByRole('img', { name: label, exact: true }).locator('svg').count(), 1)
  }

  // Changing the server interval and refreshing updates the existing chart.
  snapshot = { ...snapshot, interval_seconds: 60 }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByText('近 1 小时 · 每 60 秒采样 · 含其他程序占用', { exact: true }).waitFor()
  await page.setViewportSize({ width: 390, height: 844 })
  await panel.scrollIntoViewIfNeeded()
  await page.waitForFunction(() => {
    const graph = document.querySelector('.gpu-monitor svg')
    return graph && graph.getBoundingClientRect().width <= innerWidth
  })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile horizontal overflow')
  const mobileBox = await chart.boundingBox()
  assert.ok(mobileBox.width > 150 && mobileBox.x >= 0 && mobileBox.x + mobileBox.width <= 390)
  await panel.locator('.gpu-trend').scrollIntoViewIfNeeded()
  await panel.locator('.gpu-trend').screenshot({ path: resolve(output, 'gpu-utilization-trend-mobile.png') })

  // No supported GPU and older servers without historical GPU fields remain usable.
  snapshot = { ...snapshot, gpus: [], gpu_message: '当前未检测到可读取的 NVIDIA GPU。', history: history.map(point => ({ ...point, gpus: [] })) }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await panel.getByText('暂无历史数据，等待下一次服务器采样', { exact: true }).waitFor()
  assert.match(await panel.innerText(), /未检测到可读取/)
  snapshot = { ...snapshot, history: history.map(({ gpus, ...point }) => point) }
  const refreshed = page.waitForResponse(response => response.url().endsWith('/admin/monitor/refresh'))
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await refreshed
  await page.waitForFunction(() => !document.querySelector('.monitor-toolbar-actions button').disabled)
  assert.equal(await panel.getByRole('status').count(), 1)
  assert.deepEqual(errors, [])
  console.log('GPU trend browser checks passed: desktop/mobile, two GPUs, tooltip, interval refresh, unavailable GPU and legacy history.')
} catch (error) {
  console.error('Page errors:', errors)
  console.error('Page text:', (await page?.locator('body').innerText())?.slice(0, 2000))
  await page?.screenshot({ path: resolve(output, 'gpu-trend-browser-failure.png'), fullPage: true })
  throw error
} finally {
  await browser?.close()
  await new Promise(resolve => server.close(resolve))
}
