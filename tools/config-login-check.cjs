// Verify a new Windows installation and file-based login without touching live data.
const fs = require('fs');
const path = require('path');
const net = require('net');
const crypto = require('crypto');
const { spawn } = require('child_process');
const assert = require('node:assert/strict');
const { chromium, expect } = require('playwright/test');
const root = path.resolve(__dirname, '..');
let child, browser;
const probe = fs.mkdtempSync(path.join(root, 'dist', 'config-login-probe-'));
const executable = path.join(probe, 'gostream3d.exe');
const configFile = path.join(probe, 'config.env');
const hash = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
const readConfig = () => Object.fromEntries(fs.readFileSync(configFile, 'utf8').split(/\r?\n/).filter(line => /^GOSTREAM3D_[A-Z0-9_]+=/.test(line)).map(line => { const i = line.indexOf('='); return [line.slice(0, i), line.slice(i + 1)]; }));
const freePort = () => new Promise((resolve, reject) => {
  const server = net.createServer();
  server.once('error', reject);
  server.listen(0, '127.0.0.1', () => { const port = server.address().port; server.close(() => resolve(port)); });
});
const cleanEnv = () => Object.fromEntries(Object.entries(process.env).filter(([key]) => !/^GOSTREAM3D_/i.test(key)));
const stop = async () => {
  if (child && child.exitCode === null) await new Promise(resolve => { child.once('exit', resolve); child.kill(); });
  child = undefined;
};
const start = async (port, overrides) => {
  let output = '';
  child = spawn(executable, [], { cwd: probe, windowsHide: true, env: { ...cleanEnv(), GIN_MODE: 'release', GOSTREAM3D_NO_BROWSER: '1', ...overrides } });
  child.stdout.on('data', chunk => output += chunk);
  child.stderr.on('data', chunk => output += chunk);
  let spawnError;
  child.once('error', error => spawnError = error);
  for (let i = 0; i < 100; i++) {
    if (spawnError) throw spawnError;
    if (child.exitCode !== null) throw new Error('Probe service exited before login was available');
    try {
      const response = await fetch('http://127.0.0.1:' + port + '/healthz', { signal: AbortSignal.timeout(500) });
      if (response.ok) { assert(!output.includes('首次运行')); return; }
    } catch {}
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error('Probe service did not start directly; configuration must not wait for a setup form');
};
(async () => {
  fs.copyFileSync(path.join(root, 'dist', 'gostream3d-config-login.exe'), executable);
  const firstPort = await freePort();
  await start(firstPort, { GOSTREAM3D_HTTP_HOST: '127.0.0.1', GOSTREAM3D_HTTP_PORT: String(firstPort) });
  const original = readConfig();
  assert.equal(original.GOSTREAM3D_HTTP_HOST, '0.0.0.0');
  assert.equal(original.GOSTREAM3D_HTTP_PORT, '8888');
  assert.equal(original.GOSTREAM3D_ADMIN_USERNAME, 'admin');
  assert(original.GOSTREAM3D_ADMIN_PASSWORD.length >= 12);
  const originalHash = hash(configFile);
  browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('http://127.0.0.1:' + firstPort + '/admin');
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible();
  assert(!(await page.content()).includes('GoStream3D 首次配置'));
  await page.getByLabel('管理员账号', { exact: true }).fill(original.GOSTREAM3D_ADMIN_USERNAME);
  await page.getByLabel('管理员密码', { exact: true }).fill(original.GOSTREAM3D_ADMIN_PASSWORD);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page.locator('.console-app')).toBeVisible();
  assert.equal(hash(configFile), originalHash);
  assert.deepEqual(errors, []);
  await browser.close(); browser = undefined;
  await stop();
  console.log('Fresh standalone EXE: creates file, starts immediately, shows login and accepts file credentials');
  const secondPort = await freePort();
  const newPassword = crypto.randomBytes(24).toString('hex');
  const modified = fs.readFileSync(configFile, 'utf8')
    .replace(/^GOSTREAM3D_HTTP_HOST=.*$/m, 'GOSTREAM3D_HTTP_HOST=127.0.0.1')
    .replace(/^GOSTREAM3D_HTTP_PORT=.*$/m, 'GOSTREAM3D_HTTP_PORT=' + secondPort)
    .replace(/^GOSTREAM3D_ADMIN_USERNAME=.*$/m, 'GOSTREAM3D_ADMIN_USERNAME=operator-probe')
    .replace(/^GOSTREAM3D_ADMIN_PASSWORD=.*$/m, 'GOSTREAM3D_ADMIN_PASSWORD=' + newPassword);
  fs.writeFileSync(configFile, modified);
  const modifiedHash = hash(configFile);
  await start(secondPort, {});
  const login = async (username, password) => fetch('http://127.0.0.1:' + secondPort + '/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) });
  assert.equal((await login('operator-probe', newPassword)).status, 200);
  assert.equal((await login(original.GOSTREAM3D_ADMIN_USERNAME, original.GOSTREAM3D_ADMIN_PASSWORD)).status, 401);
  assert.equal(hash(configFile), modifiedHash);
  await stop();
  console.log('Restart: reads changed account/password/port from config.env; old credentials rejected; file preserved');
})().catch(error => { console.error(error.message); process.exitCode = 1; }).finally(async () => {
  await browser?.close();
  await stop();
  // Only remove the absolute probe directory created by this script inside this workspace.
  const relative = path.relative(path.join(root, 'dist'), probe);
  assert(relative.startsWith('config-login-probe-') && !relative.includes(path.sep) && !path.isAbsolute(relative));
  fs.rmSync(probe, { recursive: true, force: true });
});
