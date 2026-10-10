// Capture the actual console without changing project data or resource settings.
const fs = require('fs');
const path = require('path');
const assert = require('node:assert/strict');
const { chromium, expect } = require('playwright/test');
const root = path.resolve(__dirname, '..');
const out = path.join(root, 'docs/ui-preview');
fs.mkdirSync(out,{recursive:true});
const env = Object.fromEntries(fs.readFileSync(path.join(root,'dist/package-windows-amd64/config.env'),'utf8').split(/\r?\n/).filter(x=>/^[A-Z0-9_]+=/.test(x.trim())).map(x=>{const i=x.indexOf('=');return [x.slice(0,i).trim(),x.slice(i+1).trim().replace(/^(['"])(.*)\1$/,'$2')]}));
let browser;
(async()=>{
  const base=process.env.UI_BASE_URL || 'http://127.0.0.1:5173';
  browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
  const context=await browser.newContext({viewport:{width:1672,height:940},reducedMotion:'reduce'});
  const page=await context.newPage();const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/admin');
  await expect(page.getByRole('button',{name:'登录',exact:true})).toBeVisible();
  await page.screenshot({path:path.join(out,'simple-login-desktop.png')});
  await page.getByLabel('管理员账号',{exact:true}).fill(env.GOSTREAM3D_ADMIN_USERNAME);
  await page.getByLabel('管理员密码',{exact:true}).fill(env.GOSTREAM3D_ADMIN_PASSWORD);
  await page.getByRole('button',{name:'登录',exact:true}).click();
  await expect(page.locator('.console-app')).toBeVisible();
  const colorCheck=async()=>{
    assert.equal(await page.locator('.console-app').evaluate(el=>getComputedStyle(el).backgroundColor),'rgb(245, 247, 250)');
    assert.equal(await page.locator('.admin-sidebar').evaluate(el=>getComputedStyle(el).backgroundColor),'rgb(7, 21, 38)');
    assert.equal(await page.locator('.console-topbar').evaluate(el=>getComputedStyle(el).backgroundColor),'rgb(255, 255, 255)');
    for(const panel of await page.locator('.console-panel').all()){
      assert.equal(await panel.evaluate(el=>getComputedStyle(el).backgroundColor),'rgb(255, 255, 255)');
      assert.equal(await panel.evaluate(el=>getComputedStyle(el).display),'block','Panel sections must retain vertical document flow');
    }
  };
  const sections=[['创建观看链接','create'],['已创建的链接','projects'],['平台监控','monitor'],['后台设置','settings'],['外观设置','themes']];
  for(const [name,file] of sections){
    await page.locator('.admin-sidebar').getByRole('button',{name,exact:false}).click();
    if(file==='monitor')await expect(page.locator('.monitor-echart svg')).toHaveCount(3);
    if(file==='settings')await expect(page.getByLabel('本软件 CPU 使用上限',{exact:true})).toBeEnabled();
    await colorCheck();
    await page.evaluate(()=>document.querySelector('.console-main').scrollTop=0);
    await page.screenshot({path:path.join(out,'simple-'+file+'-desktop.png')});
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth&&document.querySelector('.console-main').scrollWidth<=document.querySelector('.console-main').clientWidth),name+' overflow');
    console.log(name+': light layout and screenshot passed');
  }
  for(const button of await page.locator('.console-theme-card').all()){await button.click();await colorCheck()}
  await page.reload();await colorCheck();
  await page.getByRole('button',{name:'恢复默认配色'}).click();
  await page.locator('.admin-sidebar').getByRole('button',{name:'已创建的链接',exact:false}).click();
  const editable=page.getByRole('button',{name:/^编辑 /}).filter({visible:true});
  for(const button of await editable.all()){if(await button.isEnabled()){await button.click();break}}
  if(await page.getByRole('dialog').count()){await page.screenshot({path:path.join(out,'simple-edit-desktop.png')});await page.getByRole('button',{name:'取消',exact:true}).click()}
  await page.setViewportSize({width:390,height:844});
  for(const [name,file] of sections){
    await page.getByRole('button',{name:'打开导航',exact:true}).click();
    await page.locator('.admin-sidebar').getByRole('button',{name,exact:false}).click();
    if(file==='monitor')await expect(page.locator('.monitor-echart svg')).toHaveCount(3);
    await page.evaluate(()=>document.querySelector('.console-main').scrollTop=0);
    await page.screenshot({path:path.join(out,'simple-'+file+'-mobile.png')});
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth&&document.querySelector('.console-main').scrollWidth<=document.querySelector('.console-main').clientWidth),name+' mobile overflow');
  }
  assert.deepEqual(errors,[]);
  console.log('Login, all 10 saved themes, mobile navigation and no page errors passed');
  await browser.close();
})().catch(async error=>{console.error(error.message);await browser?.close();process.exit(1)});
