// Read-only browser regression: existing links, native LAN HTTP copy/paste and viewer routes.
const fs = require('fs');
const path = require('path');
const assert = require('node:assert/strict');
const { chromium, expect } = require('playwright/test');
const root = path.resolve(__dirname, '..');
const env = Object.fromEntries(fs.readFileSync(path.join(root, 'dist/package-windows-amd64/config.env'), 'utf8').split(/\r?\n/).filter(x => /^[A-Z0-9_]+=/.test(x.trim())).map(x => {const i=x.indexOf('=');return [x.slice(0,i).trim(),x.slice(i+1).trim().replace(/^(['"])(.*)\1$/, '$2')]}));
let browser;
(async () => {
  const base = process.env.UI_BASE_URL || 'http://192.168.1.100:8888';
  const verifyServer = process.env.VERIFY_STREAM_ROUTES !== 'false';
  browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe', headless:true});
  // No clipboard permission grants: this must work with normal LAN HTTP browser settings.
  const context = await browser.newContext({viewport:{width:1672,height:940}});
  assert((await context.request.post(base+'/api/v1/auth/login', {data:{username:env.GOSTREAM3D_ADMIN_USERNAME,password:env.GOSTREAM3D_ADMIN_PASSWORD}})).ok());
  const response = await context.request.get(base+'/api/v1/admin/sessions');
  assert(response.ok());
  const sessions = (await response.json()).data;
  assert(sessions.length>0);
  const errors=[];
  const page = await context.newPage();
  page.on('pageerror',e=>errors.push(e.message));
  // Deliberately supply stale URLs, as can occur behind a remote gateway or with cached data.
  await page.route('**/api/v1/admin/sessions', route => route.fulfill({json:{code:0,data:sessions.map(s=>({...s,stream_url:'http://localhost:8888/v/'+s.token}))}}));
  await page.goto(base+'/admin#links');
  await expect(page.locator('.project-copy').first()).toBeVisible();
  assert.equal(await page.evaluate(()=>window.isSecureContext), false, 'Run this check on a LAN HTTP origin');
  assert.equal(await page.evaluate(()=>typeof navigator.clipboard), 'undefined');
  const search=page.getByRole('searchbox',{name:'搜索项目名称或地址'});
  await expect(page.locator('.project-copy')).toHaveCount(sessions.length);
  for(let i=0;i<sessions.length;i++){
    const button=page.locator('.project-copy').nth(i);
    const url=await button.getAttribute('title');
    assert(url.startsWith(base+'/Stream3D/'));
    assert(sessions.some(s=>url===base+'/Stream3D/'+encodeURIComponent(s.token)));
    assert.equal(await page.locator('.project-link>a').nth(i).getAttribute('href'),url);
    const before=await page.locator('.project-management').boundingBox();
    await button.click();
    await expect(page.locator('.console-toast[role="status"]')).toContainText('复制成功');
    assert.equal(await page.locator('.project-management').evaluate(el=>el.querySelector('textarea')!==null),false);
    assert.equal(await button.evaluate(el=>document.activeElement===el),true,'Copy restores button focus');
    assert.equal((await page.locator('.project-management').boundingBox()).y,before.y);
    await search.click();await search.press('Control+V');await expect(search).toHaveValue(url);
    await search.fill('');await expect(page.locator('.project-copy')).toHaveCount(sessions.length);
  }
  await page.screenshot({path:path.join(root,'docs/ui-preview/copy-success-lan.png')});
  await page.getByRole('button',{name:'卡片视图'}).click();
  await expect(page.locator('.project-grid-card')).toHaveCount(sessions.length);
  assert((await page.locator('.project-copy').evaluateAll(els=>els.map(e=>e.title))).every(u=>u.startsWith(base+'/Stream3D/')));
  console.log('LAN HTTP: all links follow current origin; native paste matches copied URL; list/card display, open address, focus and layout passed');
  const token=sessions[0].token;
  if(verifyServer){
    for(const item of sessions) assert.equal(item.stream_url,base+'/Stream3D/'+encodeURIComponent(item.token));
    const view=await context.request.get(base+'/Stream3D/'+token);
    assert.equal(view.status(),200);assert((await view.text()).includes('/assets/'));
    const legacy=await context.request.get(base+'/v/'+token+'?x=1',{maxRedirects:0});
    assert.equal(legacy.status(),307);assert.equal(legacy.headers().location,'/Stream3D/'+token+'?x=1');
    for(const prefix of ['/Stream3D/','/v/'])assert.equal((await context.request.get(base+prefix+'missing_token',{maxRedirects:0})).status(),404);
    console.log('Deployed API URLs, canonical viewer HTML, legacy redirect/query and missing-token 404: passed');
  }
  // Test viewer selection without creating another rendering session or disturbing live users.
  const viewer=await context.newPage();viewer.on('pageerror',e=>errors.push(e.message));
  await viewer.route('**/api/v1/sessions/'+token, route=>route.fulfill({json:{code:0,data:{transport:'jpeg',render_paused:true}}}));
  await viewer.goto(base+'/Stream3D/'+token);
  await expect(viewer.getByText('渲染已由管理员停止，请稍后刷新页面。')).toBeVisible();
  await expect(viewer.locator('.console-shell')).toHaveCount(0);
  assert.deepEqual(errors,[]);
  console.log('Stream3D selects the viewer; no extra rendering instance or uncaught browser errors: passed');
  await browser.close();
})().catch(async error=>{console.error(error);await browser?.close();process.exit(1)});
