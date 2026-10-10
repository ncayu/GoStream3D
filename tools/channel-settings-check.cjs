// UI mutations are intercepted. Never alter the running server's projects or settings.
const fs = require('fs');
const path = require('path');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { chromium, expect } = require('playwright/test');
const root = path.resolve(__dirname, '..');
const runtime = path.join(root, 'dist/package-windows-amd64');
const out = path.join(root, 'docs/ui-preview');
const files = ['config.env', 'data/sessions.json', 'data/cpu-settings.json', 'data/monitor-settings.json', 'data/channel-settings.json'];
const fingerprint = () => Object.fromEntries(files.map(name => [name, fs.existsSync(path.join(runtime, name)) ? crypto.createHash('sha256').update(fs.readFileSync(path.join(runtime, name))).digest('hex') : null]));
const env = Object.fromEntries(fs.readFileSync(path.join(runtime, 'config.env'), 'utf8').split(/\r?\n/).filter(line => /^[A-Z0-9_]+=/.test(line.trim())).map(line => { const i = line.indexOf('='); return [line.slice(0, i).trim(), line.slice(i + 1).trim().replace(/^(['"])(.*)\1$/, '$2')] }));
let browser;
(async () => {
  const base = process.env.UI_BASE_URL || 'http://127.0.0.1:8888';
  const before = fingerprint();
  browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true });
  const context = await browser.newContext({ viewport: { width: 1672, height: 940 }, reducedMotion: 'reduce' });
  assert((await context.request.post(base + '/api/v1/auth/login', { data: { username: env.GOSTREAM3D_ADMIN_USERNAME, password: env.GOSTREAM3D_ADMIN_PASSWORD } })).ok(), 'Login failed');
  const page = await context.newPage();
  const errors = [], blocked = [], writes = [];
  page.on('pageerror', error => errors.push(error.message));
  let channels = { max_channels: 3, active_channels: 1 }, failSave = false, failRead = false, created, edited;
  let sessions = [{ token: 'quota_fixture', name: '独立会话测试', target_url: 'https://example.com/3d', resolution: '1920x1080', fps: 30, render_mode: 'nvidia', concurrency_mode: 'multi', max_viewers: 3, ready_selector: '', viewers: 0, running: false, running_instances: 0, render_paused: true }];
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), method = request.method(), pathname = new URL(request.url()).pathname;
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(status === 200 ? { code: 0, data } : { code: status, msg: data }) });
    if (pathname === '/api/v1/admin/channels/settings') {
      if (method === 'GET') return failRead ? send('读取通道设置失败', 500) : send(channels);
      if (method === 'PUT') { writes.push('channels'); if (failSave) return send('当前有实例占用调整范围外的通道，请先停止相关渲染后重试。', 409); channels = { ...channels, ...request.postDataJSON() }; return send(channels) }
    }
    if (pathname === '/api/v1/admin/resources' && method === 'GET') return send({ cpu_max_percent: 50, memory_always_enabled: true, disk_cache_enabled: true, transport: 'jpeg', channel_capacity: channels.max_channels, channel_capacity_source: 'configured', hardware_capacity_detected: false });
    if (pathname === '/api/v1/admin/sessions' && method === 'GET') return send(sessions);
    if (pathname === '/api/v1/sessions' && method === 'POST') { writes.push('create'); created = request.postDataJSON(); const item = { ...sessions[0], ...created, token: 'quota_created' }; sessions.unshift(item); return send(item) }
    if (/^\/api\/v1\/admin\/sessions\/[^/]+$/.test(pathname) && method === 'PUT') { writes.push('edit'); edited = request.postDataJSON(); Object.assign(sessions.find(item => pathname.endsWith(item.token)), edited); return send({}) }
    if (method === 'GET') return route.continue();
    blocked.push(pathname); return route.abort();
  });
  const nav = async name => { if (page.viewportSize().width <= 760) await page.getByRole('button', { name: '打开导航', exact: true }).click(); await page.locator('.admin-sidebar').getByRole('button', { name, exact: false }).click() };
  const valid = locator => locator.evaluate(input => input.checkValidity());
  await page.goto(base + '/admin'); await expect(page.locator('.console-app')).toBeVisible();
  await page.getByRole('button', { name: '本机 GPU', exact: false }).click();
  await page.getByRole('button', { name: '多路独立', exact: false }).click();
  const quota = page.getByLabel('最大独立会话数');
  await expect(quota).toHaveAttribute('max', '100'); await quota.fill('10'); assert(await valid(quota));
  await expect(page.locator('.channel-quota-hint')).toContainText('如需同时运行 10 路');
  await expect(page.locator('.channel-quota-hint')).toContainText('未自动测算 GPU');
  await quota.fill('101'); assert(!await valid(quota)); await quota.fill('2.5'); assert(!await valid(quota)); await quota.fill('10');
  await page.getByLabel('项目名称', { exact: true }).fill('自定义十路');
  await page.getByLabel('原始 3D 页面 URL', { exact: false }).fill('https://example.com/twin');
  await page.getByRole('button', { name: '预加载并生成链接' }).click();
  await expect(page.locator('.console-page-heading h1')).toHaveText('已创建的链接'); assert.equal(created.max_viewers, 10);
  await page.getByRole('button', { name: '编辑 独立会话测试', exact: true }).click();
  await page.getByLabel('最大独立会话数').fill('20'); assert(await valid(page.getByLabel('最大独立会话数')));
  await page.getByRole('button', { name: '保存修改', exact: true }).click(); await expect(page.getByRole('dialog')).toHaveCount(0); assert.equal(edited.max_viewers, 20);
  await page.getByRole('button', { name: '编辑 独立会话测试', exact: true }).click();
  await page.getByRole('dialog').getByRole('link', { name: '后台设置', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0); await expect(page.locator('.console-page-heading h1')).toHaveText('后台设置');
  const card = page.locator('section[aria-labelledby="channel-settings-heading"]');
  const total = page.getByLabel('服务器总通道数');
  await expect(total).toHaveValue('3'); await expect(card).toContainText('1 / 3');
  await page.getByRole('button', { name: '会话通道', exact: true }).click();
  await total.fill('101'); assert(!await valid(total)); await total.fill('0'); assert(!await valid(total));
  await page.getByRole('button', { name: '10 路', exact: true }).click();
  await expect(card.locator('.soft-badge')).toHaveText('当前 3 路'); await expect(card).toContainText('修改尚未保存');
  failSave = true; await page.getByRole('button', { name: '保存总通道数' }).click(); await expect(card.getByRole('alert')).toContainText('请先停止'); await expect(card.locator('.soft-badge')).toHaveText('当前 3 路');
  failSave = false; await page.getByRole('button', { name: '保存总通道数' }).click(); await expect(card.locator('.soft-badge')).toHaveText('当前 10 路'); await expect(card.getByRole('status')).toContainText('立即生效');
  await expect(page.getByRole('button', { name: '保存总通道数' })).toBeDisabled();
  fs.mkdirSync(out, { recursive: true }); await card.screenshot({ path: path.join(out, 'channel-settings-desktop.png') });
  await page.reload(); await expect(total).toHaveValue('10');
  failRead = true; await page.getByRole('button', { name: '刷新状态', exact: true }).click(); await expect(card.getByRole('alert')).toContainText('读取通道设置失败'); await expect(total).toBeDisabled();
  failRead = false; await page.getByRole('button', { name: '刷新状态', exact: true }).click(); await expect(total).toBeEnabled(); await expect(total).toHaveValue('10');
  await nav('创建观看链接'); await page.getByRole('button', { name: '多路独立', exact: false }).click();
  await expect(page.locator('.channel-quota-hint')).toContainText('服务器总通道数为 10 路'); await quota.fill('20');
  await page.setViewportSize({ width: 390, height: 844 });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth && document.querySelector('.console-main').scrollWidth <= document.querySelector('.console-main').clientWidth), 'Create quota overflow');
  await quota.scrollIntoViewIfNeeded(); await page.screenshot({ path: path.join(out, 'channel-quota-mobile.png') });
  await page.locator('.channel-quota-hint a').click(); await expect(total).toBeEnabled(); await total.scrollIntoViewIfNeeded();
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth && document.querySelector('.console-main').scrollWidth <= document.querySelector('.console-main').clientWidth), 'Channel settings overflow');
  await page.screenshot({ path: path.join(out, 'channel-settings-mobile.png') });
  assert.deepEqual(fingerprint(), before, 'Runtime files were changed'); assert.equal(blocked.length, 0); assert.equal(errors.length, 0, errors.join('\n'));
  const report = { passed: true, checks: ['create 10 and edit 20 above current capacity 3', 'integer ranges', 'settings navigation from edit', 'total capacity draft/failure/success/reload', 'read failure and retry', 'resource quota updates', '390px layout', 'no runtime data changes'], interceptedWrites: writes.length };
  fs.writeFileSync(path.join(out, 'channel-settings-verification.json'), JSON.stringify(report, null, 2)); console.log(JSON.stringify(report));
  await browser.close();
})().catch(async error => { console.error(error.message); await browser?.close(); process.exit(1) });
