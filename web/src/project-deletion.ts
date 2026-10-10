export type DeleteFailure = { token: string; message: string }
export type DeleteProgress = { completed: number; total: number }
export type BulkDeleteResult = { deleted: string[]; failed: DeleteFailure[]; skipped: string[] }

// Keep the existing per-project deletion contract and worker cleanup. Each
// failure is reported independently; retry never resubmits successful items.
export async function deleteProjects(tokens: string[], remove: (token: string) => Promise<void>, signal: AbortSignal,
  onProgress: (progress: DeleteProgress) => void): Promise<BulkDeleteResult> {
  const unique = [...new Set(tokens)]
  const result: BulkDeleteResult = { deleted: [], failed: [], skipped: [] }
  onProgress({ completed: 0, total: unique.length })
  for (let index = 0; index < unique.length; index++) {
    if (signal.aborted) { result.skipped = unique.slice(index); break }
    const token = unique[index]
    try { await remove(token); result.deleted.push(token) }
    catch (cause) {
      const status = cause instanceof Error && 'status' in cause ? cause.status : undefined
      if (status === 404) result.deleted.push(token) // Another administrator already removed it.
      else result.failed.push({ token, message: signal.aborted ? '删除结果未确认，请刷新项目列表后检查。' : cause instanceof Error ? cause.message : '删除失败，请重试。' })
      if (status === 401) {
        result.skipped = unique.slice(index + 1)
        onProgress({ completed: index + 1, total: unique.length })
        break
      }
    }
    onProgress({ completed: index + 1, total: unique.length })
  }
  return result
}
