// Preview-only fixtures: all API requests and WebSockets are simulated.
const {chromium,expect}=require('playwright/test');
const assert=require('node:assert/strict'),fs=require('fs'),path=require('path');
const out=path.resolve(__dirname,'../docs/ui-preview');
(async()=>{
 const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 try {
 const page=await browser.newPage({viewport:{width:1672,height:940}});
 const errors=[],writes=[];page.on('pageerror',e=>errors.push(e.message));
 let publicStatus={transport:'jpeg',resolution:'1280x720',running:true,starting_instances:1,available_instances:0};
 let sessions=[{token:'preload_fixture',name:'预加载测试',target_url:'https://example.com/3d',resolution:'1920x1080',fps:30,render_mode:'nvidia',concurrency_mode:'multi',max_viewers:6,preload_count:3,viewers:0,running:true,running_instances:3,ready_instances:3,starting_instances:0,available_instances:3}];
 await page.route('**/api/v1/**',route=>{
  const req=route.request(),p=new URL(req.url()).pathname;
  let data={};
  if(p==='/api/v1/auth/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({username:'admin'})});
  if(p==='/api/v1/admin/resources')data={cpu_max_percent:85,memory_always_enabled:true,disk_cache_enabled:true,transport:'jpeg',channel_capacity:3};
  else if(p==='/api/v1/admin/sessions')data=sessions;
  else if(p.startsWith('/api/v1/sessions/')&&req.method()==='GET')data=publicStatus;
  else if(req.method()==='POST'&&p==='/api/v1/sessions'){const body=req.postDataJSON();writes.push({action:'create',body});data={...body,token:'created_fixture'};}
  else if(req.method()==='PUT'&&p.endsWith('/preload_fixture')){const body=req.postDataJSON();writes.push({action:'edit',body});sessions=[{...sessions[0],...body}];data=sessions[0];}
  else if(req.method()!=='GET')throw new Error('Unexpected write '+p);
  return route.fulfill({contentType:'application/json',body:JSON.stringify({code:0,data})});
 });
 await page.addInitScript(()=>{
  window.__sockets=[];
  class TestSocket {
   static OPEN=1;static CONNECTING=0;static CLOSED=3;
   readyState=0;sent=[];
   constructor(url){this.url=url;window.__sockets.push(this);}
   send(value){this.sent.push(value);}
   close(){this.readyState=3;}
  }
  window.WebSocket=TestSocket;
 });
 await page.goto('http://127.0.0.1:5175/admin#create');
 await page.getByRole('button',{name:'多路独立',exact:false}).click();
 await expect(page.locator('#preload-count')).toHaveValue('3');
 await page.locator('#max-viewers').fill('2');await expect(page.locator('#preload-count')).toHaveValue('2');
 await page.locator('#max-viewers').fill('6');await page.locator('#preload-count').fill('3');
 await page.getByPlaceholder('例如：智慧园区实时大屏').fill('预加载创建测试');
 await page.getByPlaceholder('https://example.com/digital-twin').fill('https://example.com/scene');
 fs.mkdirSync(out,{recursive:true});await page.screenshot({path:path.join(out,'preload-create.png'),fullPage:true});
 await page.getByRole('button',{name:'预加载并生成链接',exact:false}).click();
 await expect(page.getByText('就绪 3 / 预加载 3 路',{exact:false}).first()).toBeVisible();
 assert.equal(writes[0].body.preload_count,3);assert.equal(writes[0].body.max_viewers,6);
 const row=page.getByRole('row').filter({hasText:'预加载测试'});await row.getByRole('button',{name:'编辑'}).click();
 await expect(page.locator('#edit-preload')).toHaveValue('3');await page.locator('#edit-preload').fill('2');
 await page.getByRole('button',{name:'保存修改'}).click();assert.equal(writes[1].body.preload_count,2);
 await page.goto('http://127.0.0.1:5175/admin#create');await page.getByRole('button',{name:'多路独立',exact:false}).click();await page.setViewportSize({width:390,height:844});
 await expect(page.locator('#preload-count')).toBeVisible();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 await page.screenshot({path:path.join(out,'preload-create-mobile.png'),fullPage:true});
 await page.setViewportSize({width:1280,height:720});await page.goto('http://127.0.0.1:5175/Stream3D/preload_fixture');
 await expect(page.getByRole('status')).toContainText('模型加载完成后自动进入');
 await expect(page.getByRole('status')).toContainText('已等待');
 await page.screenshot({path:path.join(out,'preload-waiting.png')});
 await page.evaluate(()=>{const ws=window.__sockets[0];ws.readyState=1;ws.onopen();});
 await page.locator('canvas').click({position:{x:80,y:80}});assert.equal(await page.evaluate(()=>window.__sockets[0].sent.length),0,'input forwarded before first frame');
 await page.evaluate(async()=>{const c=document.createElement('canvas');c.width=1280;c.height=720;c.getContext('2d').fillRect(0,0,1280,720);const b=await new Promise(r=>c.toBlob(r,'image/jpeg'));window.__sockets[0].onmessage({data:b});});
 await expect(page.getByRole('status')).toHaveCount(0);await expect(page.getByLabel('已连接',{exact:true})).toBeVisible();
 await page.locator('canvas').click({position:{x:80,y:80}});assert((await page.evaluate(()=>window.__sockets[0].sent.length))>=2,'ready frame input did not forward');
 publicStatus={...publicStatus,starting_instances:0,channel_capacity_full:true};
 await page.goto('http://127.0.0.1:5175/Stream3D/full_fixture');await expect(page.getByRole('status')).toContainText('服务器渲染通道已满');
 assert.equal(await page.evaluate(()=>window.__sockets.length),0,'busy viewer created cold-start connection');
 assert.deepEqual(errors,[]);
 const report={apiWrites:writes.map(w=>({action:w.action,max_viewers:w.body.max_viewers,preload_count:w.body.preload_count})),checks:['create preload 3','quota reduction clamps preload','edit preserves independent quota','ready counts','mobile layout','visible waiting and elapsed time','input blocked before frame','input forwarded after frame','capacity full shown without opening socket'],errors};
 fs.writeFileSync(path.join(out,'preload-pool-report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report));
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
