import type { FormState } from './components/ConsoleTypes'

export function clampRoutes(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, Number.isFinite(value) ? Math.trunc(value) : min))
}

// Project quotas are independent of the shared server worker capacity.
export function normalizeRouteCounts(values: FormState): FormState {
  const concurrency_mode = values.concurrency_mode
  const max_viewers = concurrency_mode === 'single' ? 1 : clampRoutes(values.max_viewers, 2, 100)
  const preload_count = clampRoutes(values.preload_count, 1, max_viewers)
  const reserved_preloads = clampRoutes(values.reserved_preloads, 0, preload_count)
  if (concurrency_mode === values.concurrency_mode && max_viewers === values.max_viewers &&
      preload_count === values.preload_count && reserved_preloads === values.reserved_preloads) return values
  return { ...values, concurrency_mode, max_viewers, preload_count, reserved_preloads }
}

export function changeSessionMode(values: FormState, mode: FormState['concurrency_mode']): FormState {
  return normalizeRouteCounts({ ...values, concurrency_mode: mode,
    max_viewers: mode === 'single' ? 1 : values.concurrency_mode === 'single' ? 3 : values.max_viewers,
    preload_count: mode === 'single' ? 1 : values.concurrency_mode === 'single' ? 3 : values.preload_count,
  })
}
