import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const source=readFileSync(new URL('../../internal/orchestrator/gpu_depth.js',import.meta.url),'utf8');
function setup(host='campus.thingjs.com',samples24=[8,4,2]) {
 class GL {
  RENDERBUFFER=1; DEPTH_COMPONENT16=16; DEPTH_COMPONENT24=24; SAMPLES=2;
  DEPTH_COMPONENT=6402; UNSIGNED_SHORT=5123; RENDERBUFFER_BINDING=36007;
  bound={}; textureCalls=[];
  calls=[];
  getParameter(){return this.bound}
  bindRenderbuffer(target,rb){this.bound=rb}
  isRenderbuffer(rb){return !!rb}
  deleteRenderbuffer(){}
  texImage2D(...args){this.textureCalls.push(args)}
  texImage3D(...args){this.textureCalls.push(args)}
  texStorage2D(...args){this.textureCalls.push(args)}
  texStorage3D(...args){this.textureCalls.push(args)}
  getInternalformatParameter(target,format){return format===24?samples24:[8,4,2]}
  renderbufferStorage(...args){this.calls.push(args)}
  renderbufferStorageMultisample(...args){this.calls.push(args)}
 }
 const context=vm.createContext({window:{},location:{hostname:host},WebGL2RenderingContext:GL});
 vm.runInContext(source,context);
 return {gl:new GL(),context};
}
{
 const {gl,context}=setup();
 gl.renderbufferStorage(1,16,1920,1080);
 assert.deepEqual(gl.calls.at(-1),[1,24,1920,1080]);
 gl.renderbufferStorageMultisample(1,5,16,1920,1080);
 assert.deepEqual(gl.calls.at(-1),[1,5,24,1920,1080]);
 gl.renderbufferStorageMultisample(1,0,16,1920,1080);
 assert.equal(gl.calls.at(-1)[2],24);
 for(const format of [24,36012,32856,35056,36013]){gl.renderbufferStorage(1,format,3840,2160);assert.equal(gl.calls.at(-1)[1],format)}
 vm.runInContext(source,context);
 gl.renderbufferStorage(1,16,1280,720);
 assert.equal(context.window.__g3dGPUDepth.allocations,2,'reload installation must be idempotent');
}
for(const samples of [[4,2],[],[16,4,2]]){
 const {gl}=setup('campus.thingjs.com',samples);
 gl.renderbufferStorageMultisample(1,5,16,1920,1080);
 assert.equal(gl.calls.at(-1)[2],16,'preserve unsupported or differently rounded MSAA count');
}
for(const host of ['example.com','localhost','192.168.1.100','other-3d.example']){
 const {gl,context}=setup(host);
 gl.renderbufferStorage(1,16,1920,1080);assert.equal(gl.calls.at(-1)[1],24);
 assert.equal(context.window.__g3dGPUDepth.profile,'depth24');
}
vm.runInNewContext(source,{window:{},location:{hostname:'campus.thingjs.com'}});
for(const [method,args] of [
 ['texImage2D',[3553,0,16,32,32,0,6402,5123,null]],
 ['texImage2D',[3553,0,6402,32,32,0,6402,5123,null]],
 ['texImage3D',[35866,0,16,32,32,1,0,6402,5123,null]],
 ['texStorage2D',[3553,1,16,32,32]],
 ['texStorage3D',[35866,1,16,32,32,1]],
]){
 const {gl,context}=setup('other-3d.example');
 gl.renderbufferStorageMultisample(1,5,16,32,32);
 gl[method](...args);
 assert.deepEqual(gl.calls.at(-1),[1,5,16,32,32],'restore matching depth format and original sample request');
 assert.deepEqual(gl.textureCalls.at(-1),args,'leave texture type/data untouched');
 assert.equal(context.window.__g3dGPUDepth.textureCompatibleContexts,1);
 gl.renderbufferStorage(1,16,64,64);assert.equal(gl.calls.at(-1)[1],16,'future allocations stay coherent');
}
{
 const {gl,context}=setup();gl.renderbufferStorage(1,16,32,32);gl.deleteRenderbuffer(gl.bound);
 gl.texStorage2D(3553,1,16,32,32);
 assert.equal(context.window.__g3dGPUDepth.restoredAllocations,0,'deleted buffers must not be restored');
}
console.log('GPU depth precision regression tests passed');
