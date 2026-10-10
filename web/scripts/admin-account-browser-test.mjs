import assert from 'node:assert/strict'
import { mkdtemp, copyFile, writeFile, readFile, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { join, resolve, sep } from 'node:path'

const { chromium } = createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const root = fileURLToPath(new URL('../../', import.meta.url))
const cache = join(root, '.cache')
const fixture = await mkdtemp(join(cache, 'admin-account-browser-'))
const source = process.env.ADMIN_ACCOUNT_TEST_BINARY
assert.ok(source, 'Set ADMIN_ACCOUNT_TEST_BINARY to a freshly built Windows executable')
const exe = join(fixture, 'gostream3d.exe')
await copyFile(source, exe)
const probe = createServer()
await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve))
const port = probe.address().port
await new Promise(resolve => probe.close(resolve))
const base = `http://127.0.0.1:${port}`
await writeFile(join(fixture, 'config.env'), `GOSTREAM3D_RUNTIME=native\nGOSTREAM3D_HTTP_HOST=127.0.0.1\nGOSTREAM3D_HTTP_PORT=${port}\nGOSTREAM3D_ADMIN_USERNAME=admin\nGOSTREAM3D_ADMIN_PASSWORD=initial-private-password\nGOSTREAM3D_CPU_MAX_PERCENT=0\n`)
let child
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GOSTREAM3D_')))
const start = async () => {
  child = spawn(exe, [], { cwd: fixture, windowsHide: true, env: { ...env, GOSTREAM3D_NO_BROWSER: '1' }, stdio: 'ignore' })
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch(base + '/healthz')).ok) return } catch {}
    if (child.exitCode !== null) throw new Error('Fixture program exited during startup')
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  throw new Error('Fixture program did not become healthy')
}
const stop = async () => {
  if (!child || child.exitCode !== null) return
  const exited = new Promise(resolve => child.once('exit', resolve))
  child.kill(); await exited
}
let browser
try {
  await start()
  browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  const page = await context.newPage()
  const login = async (username, password) => {
    await page.locator('#admin-username').fill(username)
    await page.locator('#admin-password').fill(password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
  }
  const openAccount = async name => {
    await page.getByLabel(`用户菜单：${name}`, { exact: true }).click()
    await page.getByRole('button', { name: '修改账号密码', exact: true }).click()
    await page.getByRole('dialog', { name: '修改账号密码' }).waitFor()
  }
  const assertLinksHome = async () => {
    await page.waitForURL('**/admin#links')
    await page.getByRole('heading', { name: '已创建的链接', exact: true }).waitFor()
    assert.equal(await page.getByRole('button', { name: /^已创建的链接/ }).getAttribute('aria-current'), 'page')
  }
  await page.goto(base + '/admin')
  await login('admin', 'initial-private-password')
  await page.getByLabel('用户菜单：admin', { exact: true }).waitFor()
  await assertLinksHome()
  await page.getByRole('button', { name: '创建新链接', exact: true }).click()
  await page.waitForURL('**/admin#streams')
  await page.reload()
  await page.getByRole('heading', { name: '创建观看链接', exact: true }).waitFor()
  await page.getByLabel('用户菜单：admin', { exact: true }).click()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.locator('#admin-password').waitFor()
  await login('admin', 'initial-private-password')
  await assertLinksHome()
  await page.getByLabel('用户菜单：admin', { exact: true }).click()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.locator('#admin-password').waitFor()
  await page.goto(base + '/admin#monitor')
  await login('admin', 'initial-private-password')
  await assertLinksHome()
  const oldCookies = await context.cookies()
  await page.setViewportSize({ width: 390, height: 844 })
  await openAccount('admin')
  assert.ok(await page.evaluate(() => document.querySelector('dialog').getBoundingClientRect().width <= window.innerWidth), 'mobile dialog overflows')
  await page.keyboard.press('Escape')
  await page.getByRole('dialog').waitFor({ state: 'detached' })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await openAccount('admin')
  const output = join(root, 'output', 'playwright')
  await mkdir(output, { recursive: true })
  await page.screenshot({ path: join(output, 'admin-account-dialog.png') })
  await page.locator('#account-username').fill('operator')
  await page.locator('#account-current-password').fill('initial-private-password')
  await page.locator('#account-new-password').fill('updated-private-password')
  await page.locator('#account-confirm-password').fill('different-private-password')
  await page.getByRole('button', { name: '保存并重新登录', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '两次输入的新密码不一致' }).waitFor()
  await page.locator('#account-confirm-password').fill('updated-private-password')
  await page.locator('#account-current-password').fill('wrong-password')
  await page.getByRole('button', { name: '保存并重新登录', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '当前密码错误' }).waitFor()
  await page.locator('#account-current-password').fill('initial-private-password')
  await page.route('**/api/v1/auth/account', route => route.fulfill({ status: 500, contentType: 'application/json', body: '{"msg":"测试保存失败"}' }))
  await page.getByRole('button', { name: '保存并重新登录', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '测试保存失败' }).waitFor()
  assert.equal((await context.request.get(base + '/api/v1/auth/me')).status(), 200)
  await page.unroute('**/api/v1/auth/account')
  await page.getByRole('button', { name: '保存并重新登录', exact: true }).click()
  await page.getByRole('status').filter({ hasText: '账号密码已修改' }).waitFor()
  assert.equal(await page.locator('#admin-username').inputValue(), 'operator')
  assert.equal(await page.locator('#admin-password').inputValue(), '')
  const staleContext = await browser.newContext()
  await staleContext.addCookies(oldCookies)
  assert.equal((await staleContext.request.get(base + '/api/v1/auth/me')).status(), 401)
  await staleContext.close()
  await login('admin', 'initial-private-password')
  await page.getByRole('alert').filter({ hasText: '账号或密码错误' }).waitFor()
  await login('operator', 'updated-private-password')
  await page.getByLabel('用户菜单：operator', { exact: true }).waitFor()
  const stored = await readFile(join(fixture, 'data', 'admin-account.json'), 'utf8')
  assert.ok(!stored.includes('updated-private-password'), 'password stored in plaintext')
  assert.equal(JSON.parse(stored).username, 'operator')
  await stop(); await start()
  await page.reload()
  await login('operator', 'updated-private-password')
  await page.getByLabel('用户菜单：operator', { exact: true }).waitFor()
  await openAccount('operator')
  await page.locator('#account-current-password').fill('updated-private-password')
  await page.locator('#account-new-password').fill('second-private-password')
  await page.locator('#account-confirm-password').fill('second-private-password')
  await page.getByRole('button', { name: '保存并重新登录', exact: true }).click()
  await page.getByRole('status').filter({ hasText: '账号密码已修改' }).waitFor()
  await login('operator', 'second-private-password')
  await page.getByLabel('用户菜单：operator', { exact: true }).waitFor()
  console.log('Links homepage after initial login/re-login, explicit create navigation and reload, account dialog, validation, session revocation and restart persistence passed.')
} finally {
  await browser?.close()
  await stop()
  if (!resolve(fixture).startsWith(resolve(cache) + sep)) throw new Error('Fixture cleanup path is outside workspace cache')
  await rm(fixture, { recursive: true, force: true })
}
