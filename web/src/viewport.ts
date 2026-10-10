export type ViewportSize = { type: 'resize'; width: number; height: number }
export type ViewportBounds = { width: number; height: number }

// CPU defaults to CSS pixels; GPU viewers can request physical display pixels.
// Keep aspect ratio and bound allocation even on high-DPI or very large screens.
export function viewportSize(width: number, height: number, bounds: ViewportBounds = { width: 3840, height: 2160 }, pixelRatio = 1): ViewportSize | null {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) return null
  const density = Number.isFinite(pixelRatio) ? Math.max(1, Math.min(2, pixelRatio)) : 1
  width *= density; height *= density
  const maxWidth = Number.isFinite(bounds.width) && bounds.width >= 256 ? Math.min(3840, Math.floor(bounds.width / 8) * 8) : 3840
  const maxHeight = Number.isFinite(bounds.height) && bounds.height >= 256 ? Math.min(2160, Math.floor(bounds.height / 2) * 2) : 2160
  const scale = Math.min(maxWidth / width, maxHeight / height, Math.max(1, 256 / width, 256 / height))
  return { type: 'resize', width: Math.max(256, Math.min(maxWidth, Math.round(width * scale / 8) * 8)), height: Math.max(256, Math.min(maxHeight, Math.round(height * scale / 2) * 2)) }
}

export function watchViewport(element: HTMLElement, send: (size: ViewportSize) => void, bounds?: () => ViewportBounds, pixelRatio?: () => number) {
  let timer: number | undefined
  const sync = () => {
    if (timer !== undefined) window.clearTimeout(timer)
    timer = undefined
    const size = viewportSize(element.clientWidth, element.clientHeight, bounds?.(), pixelRatio?.())
    if (size) send(size)
  }
  const queue = () => { if (timer !== undefined) window.clearTimeout(timer); timer = window.setTimeout(sync, 300) }
  const observer = new ResizeObserver(queue)
  observer.observe(element)
  return { sync, stop: () => { observer.disconnect(); if (timer !== undefined) window.clearTimeout(timer) } }
}

export const resolutionLabel = (value: string) => value === 'adaptive' ? '自适应观看窗口' : value.replace('x', ' × ')
