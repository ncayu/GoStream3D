(() => {
  'use strict';
  if (window.__g3dCPUEffects) return;
  const status = window.__g3dCPUEffects = {
    profile: 'simplified', contexts: 0, thingJS: false, error: null
  };
  // MSAA requires context recreation: install before any page WebGL requests.
  // Keep all other context attributes, canvas dimensions and pixel ratio intact.
  const getContext = HTMLCanvasElement.prototype.getContext;
  HTMLCanvasElement.prototype.getContext = function(type, attributes, ...rest) {
    if (type === 'webgl' || type === 'webgl2' || type === 'experimental-webgl') {
      const context = getContext.call(this, type, { ...attributes, antialias: false }, ...rest);
      if (context) status.contexts++;
      return context;
    }
    return getContext.call(this, type, attributes, ...rest);
  };
  // ThingJS adaptation is limited to its known site and detected renderer API.
  // Other applications keep their scene graph, effects and interaction logic.
  if (location.hostname !== 'campus.thingjs.com') return;
  const disabled = {
    enable: true, MSAA: { enable: false }, FXAA: { enable: false },
    bloom: { enable: false }, screenSpaceAmbientOcclusion: { enable: false },
    screenSpaceReflection: { enable: false }, colorCorrection: { enable: false },
    dof: { enable: false }
  };
  const apply = () => {
    const app = window.app;
    if (!app?.renderer || !app?.renderEffects || !('postEffect' in app)) return;
    try {
      // Keep the public postEffect container available: the source camera's
      // zoom tween dereferences app.postEffect.postEffect.enable. Disabling
      // the whole container returns null and breaks that interaction.
      const effects = app.postEffect;
      const post = effects?.postEffect || {};
      if (!effects || post.enable !== true || effects.temporalSuperSampling?.enable ||
          Object.values(post).some(effect => effect && typeof effect === 'object' && effect.enable === true)) {
        const next = { ...post, ...disabled };
        for (const [name, effect] of Object.entries(post)) {
          if (effect && typeof effect === 'object' && 'enable' in effect) next[name] = { ...effect, enable: false };
        }
        app.postEffect = { postEffect: next, temporalSuperSampling: { enable: false } };
      }
      const lighting = app.lighting;
      if (lighting && ['mainLight', 'secondaryLight', 'tertiaryLight'].some(k => lighting[k]?.shadow)) {
        const next = { ...lighting };
        for (const k of ['mainLight', 'secondaryLight', 'tertiaryLight']) {
          if (next[k]) next[k] = { ...next[k], shadow: false };
        }
        app.lighting = next;
      }
      if (app.renderer.shadowMap) app.renderer.shadowMap.enabled = false;
      // This compatibility switch suppresses the outline pass without removing
      // selected objects, picking handlers, labels or overlay UI.
      const renderer = app.rendererManager?._mainRenderer;
      if (typeof renderer?.enableOutlines === 'boolean') renderer.enableOutlines = false;
      if (typeof renderer?.schemeAutoMSAA === 'boolean') renderer.schemeAutoMSAA = false;
      status.thingJS = true;
      status.error = null;
    } catch (error) {
      status.error = String(error?.message || error);
    }
  };
  apply();
  // Recheck only once a second, restoring the profile after scene reinitialization.
  // Assign effect configuration only when the source actually enables it again.
  setInterval(apply, 1000);
})();
