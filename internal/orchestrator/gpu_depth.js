(() => {
  'use strict';
  if (typeof WebGL2RenderingContext === 'undefined' || window.__g3dGPUDepth) return;
  const status = window.__g3dGPUDepth = { profile: 'depth24', allocations: 0, multisampleAllocations: 0, unsupportedMultisample: 0, textureCompatibleContexts: 0, restoredAllocations: 0 };
  const proto = WebGL2RenderingContext.prototype;
  const storage = proto.renderbufferStorage;
  const multisample = proto.renderbufferStorageMultisample;
  const support = new WeakMap();
  const contexts = new WeakMap();
  const state = gl => {
    let s = contexts.get(gl);
    if (!s) { s = { texture16: false, buffers: new WeakMap(), refs: new Set() }; contexts.set(gl, s); }
    return s;
  };
  const remember = (gl, args, isMultisample, upgraded) => {
    const rb = gl.getParameter?.(gl.RENDERBUFFER_BINDING);
    if (!rb) return;
    const s = state(gl), previous = s.buffers.get(rb);
    if (previous) { s.refs.delete(previous.ref); s.buffers.delete(rb); }
    if (upgraded) {
      if (s.refs.size > 256) for (const ref of s.refs) if (!ref.deref()) s.refs.delete(ref);
      const ref = new WeakRef(rb);
      s.buffers.set(rb, { args, isMultisample, ref }); s.refs.add(ref);
    }
  };
  const preserveTextureDepth = gl => {
    const s = state(gl);
    if (s.texture16) return;
    s.texture16 = true; status.textureCompatibleContexts++;
    // Depth blits require matching formats. Keep 16-bit depth-texture pipelines
    // coherent without changing texture types or intercepting shader data.
    // A restored buffer is refilled by the engine's next normal render pass.
    const bound = gl.getParameter(gl.RENDERBUFFER_BINDING);
    try {
      for (const ref of s.refs) {
        const rb = ref.deref();
        if (!rb || !gl.isRenderbuffer(rb)) continue;
        const allocation = s.buffers.get(rb);
        if (!allocation) continue;
        gl.bindRenderbuffer(gl.RENDERBUFFER, rb);
        (allocation.isMultisample ? multisample : storage).apply(gl, allocation.args);
        s.buffers.delete(rb); status.restoredAllocations++;
      }
    } finally { gl.bindRenderbuffer(gl.RENDERBUFFER, bound); s.refs.clear(); }
  };
  // WebGL2 guarantees DEPTH_COMPONENT24. Keep depth/stencil and colour formats
  // intact; only upgrade 16-bit depth-only renderbuffers, regardless of engine.
  proto.renderbufferStorage = function(target, format, width, height) {
    const original = [target, format, width, height];
    let upgraded = false;
    if (target === this.RENDERBUFFER && format === this.DEPTH_COMPONENT16 && !state(this).texture16) {
      format = this.DEPTH_COMPONENT24;
      upgraded = true; status.allocations++;
    }
    const result = storage.call(this, target, format, width, height);
    if (target === this.RENDERBUFFER) remember(this, original, false, upgraded);
    return result;
  };
  proto.renderbufferStorageMultisample = function(target, samples, format, width, height) {
    const original = [target, samples, format, width, height];
    let upgraded = false;
    if (target === this.RENDERBUFFER && format === this.DEPTH_COMPONENT16 && samples >= 0 && !state(this).texture16) {
      let formats = support.get(this);
      if (!formats) {
        try {
          formats = [this.DEPTH_COMPONENT16, this.DEPTH_COMPONENT24].map(f =>
            Array.from(this.getInternalformatParameter(this.RENDERBUFFER, f, this.SAMPLES)));
          support.set(this, formats);
        } catch { formats = [[], []]; }
      }
      // GL may round a requested count up (ThingJS requests 5 -> NVIDIA uses 8).
      // Upgrade only when both formats produce the same actual sample count.
      const actual = counts => samples === 0 ? 0 : Math.min(...counts.filter(n => n >= samples));
      const before = actual(formats[0]), after = actual(formats[1]);
      if (Number.isFinite(after) && before === after) {
        format = this.DEPTH_COMPONENT24;
        upgraded = true; status.multisampleAllocations++;
      } else status.unsupportedMultisample++;
    }
    const result = multisample.call(this, target, samples, format, width, height);
    if (target === this.RENDERBUFFER) remember(this, original, true, upgraded);
    return result;
  };
  for (const [method, typeIndex] of [['texImage2D', 7], ['texImage3D', 8], ['texStorage2D', -1], ['texStorage3D', -1]]) {
    const original = proto[method];
    if (!original) continue;
    proto[method] = function(...args) {
      const format = args[2], type = args.length === 6 && method === 'texImage2D' ? args[4] : args[typeIndex];
      if (format === this.DEPTH_COMPONENT16 || (format === this.DEPTH_COMPONENT && type === this.UNSIGNED_SHORT)) preserveTextureDepth(this);
      return original.apply(this, args);
    };
  }
  const remove = proto.deleteRenderbuffer;
  if (remove) proto.deleteRenderbuffer = function(rb) {
    const s = contexts.get(this), allocation = rb && s?.buffers.get(rb);
    if (allocation) { s.refs.delete(allocation.ref); s.buffers.delete(rb); }
    return remove.call(this, rb);
  };
})();
