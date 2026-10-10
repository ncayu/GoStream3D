export type Session = {
	show_fps?: boolean
  frame_policy?: 'interactive' | 'animation'; reserved_preloads?: number; preload_suspended?: boolean
  token: string; name: string; target_url: string; resolution: string; fps: number
  render_mode: 'cpu' | 'nvidia'; concurrency_mode: 'single' | 'multi'; max_viewers: number; preload_count?: number
  ready_selector?: string; viewers: number; running_instances: number; running: boolean
  ready_instances?: number; available_instances?: number; starting_instances?: number; pool_error?: string
  started_at?: string; preload_error?: string; render_paused?: boolean; stream_url?: string
}
export type FormState = {
	show_fps: boolean
  frame_policy: 'interactive' | 'animation'; reserved_preloads: number
  name: string; target_url: string; resolution: string; fps: number
  render_mode: 'cpu' | 'nvidia'; concurrency_mode: 'single' | 'multi'
  max_viewers: number; preload_count: number; ready_selector: string
}
export type ResourcePolicy = {
  cpu_max_percent: number; cpu_limit_supported?: boolean; memory_enabled: boolean
  disk_cache_enabled: boolean; transport: string; channel_capacity: number
}
// Always share through the origin used to open this console, including LAN IP,
// port and HTTPS scheme. Server/legacy URLs must not send viewers to localhost.
export const streamURL = (item: Pick<Session, 'token'>) => `${window.location.origin}/Stream3D/${encodeURIComponent(item.token)}`
