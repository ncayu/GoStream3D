// JSON API responses are small. Buffer the body within the deadline so callers
// cannot get stuck in response.json() after headers have already arrived.
export async function fetchWithTimeout(path: string, init: RequestInit = {},
  timeoutMs = !init.method || init.method === 'GET' ? 10000 : 30000): Promise<Response> {
  const controller = new AbortController()
  const external = init.signal
  const cancel = () => controller.abort(external?.reason)
  if (external?.aborted) cancel()
  else external?.addEventListener('abort', cancel, { once: true })
  let timedOut = false
  const timer = setTimeout(() => { timedOut = true; controller.abort() }, timeoutMs)
  try {
    const response = await fetch(path, { ...init, signal: controller.signal })
    const body = await response.arrayBuffer()
    controller.signal.throwIfAborted()
    return new Response([204, 205, 304].includes(response.status) || init.method === 'HEAD' ? null : body, {
      status: response.status, statusText: response.statusText, headers: response.headers,
    })
  } catch (cause) {
    if (external?.aborted) throw external.reason
    if (timedOut) throw new Error('请求超时，请刷新状态后重试。')
    if (cause instanceof TypeError) throw new Error('无法连接服务，请检查连接后重试。')
    throw cause
  } finally {
    clearTimeout(timer)
    external?.removeEventListener('abort', cancel)
  }
}
