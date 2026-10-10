// Exercise the console against intercepted API mutations; saved projects and resource limits stay intact.
const fs = require('fs');
const path = require('path');
const assert = require('node:assert/strict');
const { chromium, expect } = require('playwright/test');
const root = path.resolve(__dirname, '..');
const env = Object.fromEntries(fs.readFileSync(path.join(root,'dist/package-windows-amd64/config.env'),'utf8').split(/\r?\n/).filter(x => /^[A-Z0-9_]+=/.test(x.trim())).map(x => { const i=x.indexOf('=');return [x.slice(0,i).trim(),x.slice(i+1).trim().replace(/^(['"])(.*)\1$/,'$2')] }));
let browser;
(async () => {
  const base = process.env.UI_BASE_URL || 'http://127.0.0.1:5173';
  browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
  const context = await browser.newContext({viewport:{width:1672,height:940},reducedMotion:'reduce'});
  assert((await context.request.post(base+'/api/v1/auth/login',{data:{username:env.GOSTREAM3D_ADMIN_USERNAME,password:env.GOSTREAM3D_ADMIN_PASSWORD}})).ok());
  const realSessions=(await (await context.request.get(base+'/api/v1/admin/sessions')).json()).data;
  const realCPU=(await (await context.request.get(base+'/api/v1/admin/cpu/settings')).json()).data;
  const realSampling=(await (await context.request.get(base+'/api/v1/admin/monitor/settings')).json()).data;
  assert.equal(realSessions.length,5);
  let sessions=JSON.parse(JSON.stringify(realSessions));
  for(let i=0;i<7;i++)sessions.push({...sessions[i%5],token:'ui_mock_'+i,name:'验证项目 '+i});
  sessions[0]={...sessions[0],running:true,render_paused:false,running_instances:1,viewers:0};
  sessions[1]={...sessions[1],running:true,render_paused:false,running_instances:1,viewers:1};
  let cpu={...realCPU}, sampling={...realSampling};
  let failCPU=false,failDelete=false,created,edited; const writes=[]; const blocked=[];const errors=[];
  const page = await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/api/v1/**',async route=>{
    const request=route.request(); const method=request.method();const url=new URL(request.url());const pathname=url.pathname;
    const send=(data,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(status===200?{code:0,data}: {code:status,msg:data})});
    if(pathname==='/api/v1/admin/sessions' && method==='GET')return send(sessions);
    if(pathname==='/api/v1/admin/channels/settings' && method==='GET')return send({max_channels:3,active_channels:0});
    if(pathname==='/api/v1/admin/cpu/settings'){
      if(method==='GET')return send(cpu);
      if(method==='PUT'){writes.push('cpu');if(failCPU)return send('验证：配额保存失败',500);cpu={...cpu,...request.postDataJSON()};return send(cpu)}
    }
    if(pathname==='/api/v1/admin/monitor/settings'){
      if(method==='GET')return send(sampling);
      if(method==='PUT'){writes.push('sampling');sampling={...sampling,...request.postDataJSON()};return send(sampling)}
    }
    if(pathname==='/api/v1/sessions'&&method==='POST'){
      created=request.postDataJSON();writes.push('create');const item={...created,token:'ui_created',stream_url:base+'/Stream3D/ui_created',running:false,render_paused:false,running_instances:0,viewers:0};sessions.unshift(item);return send(item);
    }
    const match=pathname.match(/^\/api\/v1\/admin\/sessions\/([^/]+)(?:\/(start|stop|warm))?$/);
    if(match&&method!=='GET'){
      writes.push(method+':session');const item=sessions.find(x=>x.token===match[1]);
      if(method==='PUT'){edited=request.postDataJSON();Object.assign(item,edited);return send(item)}
      if(method==='DELETE'){if(failDelete)return send('验证：删除失败',500);sessions=sessions.filter(x=>x.token!==match[1]);return send({})}
      if(method==='POST'){Object.assign(item,{render_paused:match[2]==='stop',running:match[2]!=='stop',running_instances:match[2]==='stop'?0:1});return send(item)}
    }
    if(method==='GET')return route.continue();
    blocked.push(pathname);return route.abort();
  });
  const nav=async name=>{if(page.viewportSize().width<=760)await page.getByRole('button',{name:'打开导航',exact:true}).click();await page.locator('.admin-sidebar').getByRole('button',{name,exact:false}).click()};
  const rows=()=>page.locator('.project-table tbody tr');
  await page.goto(base+'/admin');await page.locator('.console-app').waitFor();
  await page.getByLabel('项目名称',{exact:true}).fill('独立会话验证');
  await page.getByLabel('原始 3D 页面 URL',{exact:false}).fill('https://example.com/digital-twin');
  await page.getByRole('button',{name:'本机 GPU',exact:false}).click();
  await page.getByRole('button',{name:'多路独立',exact:false}).click();
  await page.getByLabel('最大独立会话数').fill('3');
  await expect(page.getByLabel('最大独立会话数')).toHaveAttribute('max','3');
  await page.getByLabel('目标帧率').selectOption('120');
  await expect(page.getByLabel('目标帧率')).toHaveValue('120');
  await page.getByLabel('分辨率',{exact:true}).selectOption('2560x1440');
  await expect(page.getByLabel('目标帧率')).toHaveValue('30');
  await expect(page.getByLabel('目标帧率').locator('option[value="120"]')).toBeDisabled();
  await page.getByRole('button',{name:'预加载并生成链接'}).click();
  await expect(page.getByRole('heading',{name:'已创建的链接',exact:true})).toBeVisible();
  assert.equal(created.max_viewers,10);assert.equal(created.concurrency_mode,'multi');assert.equal(created.render_mode,'nvidia');assert.equal(created.resolution,'2560x1440');assert.equal(created.fps,30);
  await expect(rows()).toHaveCount(10);
  await page.getByRole('button',{name:'下一页',exact:true}).click();await expect(rows()).toHaveCount(3);
  await page.getByLabel('每页项目数').selectOption('20');await expect(rows()).toHaveCount(13);
  await page.getByLabel('搜索项目名称或地址').fill('智慧园区');await expect(rows()).toHaveCount(1);
  await page.getByLabel('筛选渲染方式').selectOption('nvidia');await expect(page.getByRole('heading',{name:'没有匹配的项目'})).toBeVisible();
  await page.getByRole('button',{name:'清除筛选'}).click();await expect(rows()).toHaveCount(13);
  await page.getByLabel('筛选观看模式').selectOption('multi');assert((await rows().count())>=3);
  await page.getByLabel('筛选观看模式').selectOption('all');
  await page.getByRole('button',{name:/已就绪/}).click();await expect(rows()).toHaveCount(2);
  await expect(page.getByRole('button',{name:'编辑 '+sessions.find(s=>s.viewers===1).name,exact:true})).toBeDisabled();
  await page.getByRole('button',{name:/^全部/}).click();
  await page.getByRole('button',{name:'卡片视图',exact:true}).click();await expect(page.locator('.project-grid-card')).toHaveCount(13);
  await page.getByRole('button',{name:'列表视图',exact:true}).click();
  const original=sessions.find(s=>s.name==='智慧园区'); const link=base+'/Stream3D/'+original.token;
  await page.getByRole('button',{name:'编辑 智慧园区',exact:true}).click();
  await expect(page.getByLabel('项目名称',{exact:true})).toBeFocused();
  await page.getByLabel('项目名称',{exact:true}).fill('智慧园区 已编辑');
  await page.getByRole('button',{name:'保存修改',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);
  assert.equal(edited.name,'智慧园区 已编辑');assert.equal(base+'/Stream3D/'+original.token,link);
  await expect(page.getByRole('button',{name:'复制 智慧园区 已编辑 的观看链接'})).toHaveAttribute('title',link);
  const readyRow=rows().filter({hasText:sessions.find(s=>s.token===realSessions[0].token).name});
  await readyRow.getByRole('button',{name:'停止渲染'}).click();await expect(readyRow.getByRole('button',{name:'启动渲染'})).toBeVisible();
  await readyRow.getByRole('button',{name:'启动渲染'}).click();await expect(readyRow.getByRole('button',{name:'停止渲染'})).toBeVisible();
  await page.getByLabel('搜索项目名称或地址').fill('智慧园区 已编辑');
  await page.getByLabel('智慧园区 已编辑 的更多操作').click();
  const menu=page.locator('.project-more[open] .dropdown-menu');await expect(menu).toBeVisible();
  const rect=await menu.boundingBox();assert(rect.x>=0&&rect.y>=0&&rect.x+rect.width<=1672&&rect.y+rect.height<=940);
  await menu.getByRole('button',{name:'删除项目',exact:true}).click();await expect(page.getByRole('alertdialog')).toBeVisible();
  await page.getByRole('button',{name:'取消',exact:true}).click();await expect(page.getByRole('alertdialog')).toHaveCount(0);
  await page.getByLabel('智慧园区 已编辑 的更多操作').click();await page.locator('.project-more[open]').getByRole('button',{name:'删除项目',exact:true}).click();
  failDelete=true;await page.getByRole('button',{name:'确认删除'}).click();await expect(page.getByRole('alertdialog').getByRole('alert')).toContainText('删除失败');
  failDelete=false;await page.getByRole('button',{name:'确认删除'}).click();await expect(page.getByRole('alertdialog')).toHaveCount(0);
  assert(!sessions.find(s=>s.token===original.token));
  await nav('后台设置');await expect(page.getByLabel('本软件 CPU 使用上限',{exact:true})).toHaveValue(String(realCPU.cpu_max_percent));
  await page.getByRole('button',{name:'40%',exact:true}).click();await expect(page.locator('.settings-limit-value strong')).toHaveText(String(realCPU.cpu_max_percent)+'%');
  await expect(page.getByText('修改尚未保存',{exact:true})).toHaveCount(1);
  failCPU=true;await page.getByRole('button',{name:'保存 CPU 上限'}).click();await expect(page.getByRole('alert')).toContainText('配额保存失败');await expect(page.locator('.settings-limit-value strong')).toHaveText(String(realCPU.cpu_max_percent)+'%');
  failCPU=false;await page.getByRole('button',{name:'保存 CPU 上限'}).click();await expect(page.locator('.settings-limit-value strong')).toHaveText('40%');
  await expect(page.getByRole('button',{name:'保存 CPU 上限'})).toBeDisabled();
  await page.getByRole('button',{name:'120 秒',exact:true}).click();await expect(page.locator('.console-summary')).toContainText(realSampling.interval_seconds+' 秒 / 次');
  await page.getByRole('button',{name:'保存采样设置'}).click();await expect(page.locator('.console-summary')).toContainText('120 秒 / 次');
  await nav('外观设置');await expect(page.locator('.console-theme-card')).toHaveCount(10);
  for(const name of ['海蓝','紫色','青色','橙色','玫红','绿色','灰色','暖金','浅蓝']){
    await page.getByRole('button',{name:new RegExp('^'+name)}).click();await expect(page.locator('.theme-preference-info')).toContainText(name);
  }
  await page.screenshot({path:path.join(root,'docs/ui-preview/themes-light-desktop.png')});
  await page.reload();await expect(page.locator('.console-app')).toHaveAttribute('data-theme','glacier');
  await page.getByRole('button',{name:'恢复默认配色'}).click();await expect(page.locator('.console-app')).toHaveAttribute('data-theme','blue-tech');
  await page.getByRole('button',{name:'收起导航',exact:true}).click();await expect(page.locator('.admin-sidebar')).toHaveCSS('width','78px');
  await page.getByRole('button',{name:'展开导航',exact:true}).click();
  await page.setViewportSize({width:390,height:844});
  for(const name of ['创建观看链接','已创建的链接','平台监控','后台设置','外观设置']){
    await nav(name);await expect(page.locator('.console-page-heading h1')).toHaveText(name);
    await expect(page.locator('.admin-sidebar')).toHaveCSS('visibility','hidden');
    if(name==='平台监控') await expect(page.locator('.monitor-echart svg')).toHaveCount(3);
    const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth||document.querySelector('.console-main').scrollWidth>document.querySelector('.console-main').clientWidth);assert(!overflow,name+' has outer horizontal overflow');
  }
  await page.getByRole('button',{name:'打开导航',exact:true}).click();await expect(page.locator('.admin-sidebar')).toHaveCSS('visibility','visible');
  await page.getByRole('button',{name:'关闭导航',exact:true}).click();await expect(page.locator('.admin-sidebar')).toHaveCSS('visibility','hidden');
  assert.equal(errors.length,0,errors.join('\n'));assert.equal(blocked.length,0,blocked.join('\n'));
  const afterSessions=(await (await context.request.get(base+'/api/v1/admin/sessions')).json()).data;
  const afterCPU=(await (await context.request.get(base+'/api/v1/admin/cpu/settings')).json()).data;
  const afterSampling=(await (await context.request.get(base+'/api/v1/admin/monitor/settings')).json()).data;
  assert.deepEqual(afterSessions.map(x=>[x.token,x.name,x.render_paused]),realSessions.map(x=>[x.token,x.name,x.render_paused]));assert.deepEqual(afterCPU,realCPU);assert.deepEqual(afterSampling,realSampling);
  const report={passed:true,checks:['creation summary and independent session payload','resolution and FPS gate','pagination and search/render/session/status filters','list/card switch','edit and stable viewing URL','start/stop','menu positioning and deletion failure/retry','CPU draft/saved state and failed/successful save','monitor sampling draft/save','10 themes, persistence and reset','desktop collapse and all 5 mobile pages','3 live ECharts SVG trends','no browser errors or outer horizontal overflow','saved server data unchanged'],interceptedWrites:writes.length,realProjectCount:realSessions.length,cpuPercent:realCPU.cpu_max_percent,sampleSeconds:realSampling.interval_seconds};
  fs.writeFileSync(path.join(root,'docs/ui-preview/verification.json'),JSON.stringify(report,null,2));
  console.log(JSON.stringify(report));await browser.close();
})().catch(async error=>{console.error(error);await browser?.close();process.exit(1)});

