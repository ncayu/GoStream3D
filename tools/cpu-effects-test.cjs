// Verify policy compatibility with source configuration updates, not only startup.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('internal/orchestrator/cpu_effects.js', 'utf8');
function fixture(hostname) {
  let effects = { postEffect: { enable: true, MSAA: { enable: true }, colorCorrection: { enable: true }, customEffect: { enable: true, strength: .3 } }, temporalSuperSampling: { enable: true } };
  let assignments = 0;
  class Canvas {
    constructor() { this.width = 1920; this.height = 1080; }
    getContext(type, attributes) { return { type, attributes }; }
  }
  const app = {
    pixelRatio: 1, scene: { modelCount: 180 }, selected: 'building',
    renderer: { shadowMap: { enabled: true } }, renderEffects: {},
    rendererManager: { _mainRenderer: { enableOutlines: true, enableOverlays: true, schemeAutoMSAA: true } },
    lighting: { mainLight: { shadow: true, intensity: .5 }, ambientLight: { intensity: .4 } },
    get postEffect() { return effects; },
    set postEffect(value) { assignments++; effects = value.postEffect.enable ? value : null; }
  };
  const ticks = [];
  const context = { window: { app }, location: { hostname }, HTMLCanvasElement: Canvas, setInterval: cb => ticks.push(cb) };
  vm.runInNewContext(source, context);
  return { context, app, ticks, assignments: () => assignments, enableAgain: () => { effects = { postEffect: { enable: true, MSAA: { enable: true } } }; } };
}
const campus = fixture('campus.thingjs.com');
const canvas = new campus.context.HTMLCanvasElement();
const attributes = { antialias: true, alpha: true, stencil: false, preserveDrawingBuffer: true };
const gl = canvas.getContext('webgl2', attributes);
assert.equal(gl.attributes.antialias, false);
for (const k of ['alpha', 'stencil', 'preserveDrawingBuffer']) assert.equal(gl.attributes[k], attributes[k]);
assert.equal(attributes.antialias, true);
assert.equal(canvas.getContext('2d', attributes).attributes, attributes);
assert.deepEqual([canvas.width, canvas.height, campus.app.pixelRatio], [1920, 1080, 1]);
assert.equal(campus.app.postEffect.postEffect.enable, true, 'camera zoom must retain its expected container');
assert.equal(campus.app.postEffect.postEffect.MSAA.enable, false);
assert.equal(campus.app.postEffect.postEffect.customEffect.enable, false);
assert.equal(campus.app.postEffect.postEffect.customEffect.strength, .3);
assert.equal(campus.app.lighting.mainLight.shadow, false);
assert.equal(campus.app.lighting.mainLight.intensity, .5);
assert.equal(campus.app.renderer.shadowMap.enabled, false);
assert.equal(campus.app.rendererManager._mainRenderer.enableOutlines, false);
assert.equal(campus.app.rendererManager._mainRenderer.enableOverlays, true);
assert.equal(campus.app.rendererManager._mainRenderer.schemeAutoMSAA, false);
assert.equal(campus.app.selected, 'building');
assert.equal(campus.app.scene.modelCount, 180);
campus.ticks[0]();
assert.equal(campus.assignments(), 1, 'idle checks must not rebuild effects');
campus.enableAgain(); campus.ticks[0]();
assert.equal(campus.assignments(), 2, 'source reinitialization must reapply policy');
assert.equal(campus.app.postEffect.postEffect.MSAA.enable, false);
vm.runInNewContext(source, campus.context);
assert.equal(campus.ticks.length, 1, 'reinstallation must not add another timer');
const other = fixture('other.example');
assert.equal(other.app.postEffect.postEffect.enable, true);
assert.equal(other.app.renderer.shadowMap.enabled, true);
assert.equal(other.ticks.length, 0);
console.log('CPU effect policy: full resolution, context attributes, picking/overlays, idle checks, source reinitialization and unrelated sites passed.');
