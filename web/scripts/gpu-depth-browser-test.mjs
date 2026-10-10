import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const {chromium}=createRequire(import.meta.url)(process.env.PLAYWRIGHT_MODULE_PATH||'playwright');
const scene=await readFile(new URL('./fixtures/gpu-depth-scene.html',import.meta.url));
const server=createServer((req,res)=>{res.setHeader('Content-Type','text/html');res.end(scene)});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const base=`http://127.0.0.1:${server.address().port}/scene`;
const reservation=createServer();await new Promise(resolve=>reservation.listen(0,'127.0.0.1',resolve));
const cdpPort=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
const browser=await chromium.launch({channel:process.env.PLAYWRIGHT_CHANNEL||'msedge',headless:true,args:[`--remote-debugging-port=${cdpPort}`,'--use-angle=swiftshader','--enable-unsafe-swiftshader','--site-per-process']});
const output=new URL('../../output/generic-depth/',import.meta.url);await mkdir(output,{recursive:true});
try{
  const baseline=await browser.newPage();await baseline.goto(base);
  const before=await baseline.evaluate(()=>depthProbe());
  assert.equal(before.bits,16);assert.equal(before.error,0);assert.deepEqual(before.pixel,[255,0,0,255],'baseline must reproduce depth conflict');
  await baseline.screenshot({path:fileURLToPath(new URL('depth16.png',output))});await baseline.close();
  const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const result=await promisify(execFile)('go',['test','./internal/orchestrator','-run','^TestGPUPageRealBrowser$','-count=1'],{
    cwd:fileURLToPath(new URL('../../',import.meta.url)),timeout:60000,
    env:{...process.env,GOSTREAM3D_GPU_DEPTH_TEST_URL:base,GOSTREAM3D_GPU_DEPTH_CDP_PORT:String(cdpPort),GOSTREAM3D_GPU_DEPTH_REPORT:fileURLToPath(new URL('browser-checks.json',output))}});
  console.log(result.stdout);
  await page.screenshot({path:fileURLToPath(new URL('depth24.png',output))});
  const {checks}=JSON.parse(await readFile(new URL('browser-checks.json',output),'utf8'));assert.deepEqual(errors,[]);
  await writeFile(new URL('browser-regression.json',output),JSON.stringify({before,checks,errors},null,2));
  console.log(JSON.stringify({baseline:before.bits,checks:checks.map(c=>({label:c.label,bits:c.bits,pixel:c.pixel,msaa:c.msaa})),errors}));
}finally{await browser.close();await new Promise(resolve=>server.close(resolve))}
