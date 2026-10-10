export type PlaybackSample = { at: number; total: number; dropped: number; width: number; height: number }
export type InboundSample = { id: string; timestamp: number; framesReceived?: number; framesDecoded?: number; framesDropped?: number; bytesReceived?: number; totalDecodeTime?: number; jitterBufferDelay?: number; jitterBufferEmittedCount?: number; packetsLost?: number; packetsReceived?: number }
export type WebRTCPerformance = {
  width: number; height: number; codec: string; sample_seconds: number
  receive_fps: number | null; display_fps: number | null; bitrate_mbps: number | null
  decode_ms: number | null; buffer_ms: number | null; rtt_ms: number | null
  packets_lost: number | null; frames_dropped: number | null
}
const delta = (before: number | undefined, after: number | undefined): number | null =>
  before !== undefined && after !== undefined && Number.isFinite(before) && Number.isFinite(after) && after >= before ? after - before : null

export function webrtcPerformance(before: InboundSample | undefined, after: InboundSample, oldPlayback: PlaybackSample | undefined, playback: PlaybackSample, codec: string, rtt?: number): WebRTCPerformance | null {
  if (!before || before.id !== after.id) return null
  const seconds = (after.timestamp - before.timestamp) / 1000
  if (!Number.isFinite(seconds) || seconds < .25 || seconds > 30 || (before.bytesReceived !== undefined && after.bytesReceived !== undefined && after.bytesReceived < before.bytesReceived)) return null
  const frames = delta(before.framesReceived, after.framesReceived), bytes = delta(before.bytesReceived, after.bytesReceived)
  const decoded = delta(before.framesDecoded, after.framesDecoded), decode = delta(before.totalDecodeTime, after.totalDecodeTime)
  const emitted = delta(before.jitterBufferEmittedCount, after.jitterBufferEmittedCount), buffer = delta(before.jitterBufferDelay, after.jitterBufferDelay)
  const total = oldPlayback ? delta(oldPlayback.total, playback.total) : null, dropped = oldPlayback ? delta(oldPlayback.dropped, playback.dropped) : null
  const elapsed = oldPlayback ? (playback.at - oldPlayback.at) / 1000 : 0
  const loss = before.packetsLost !== undefined && after.packetsLost !== undefined && Number.isFinite(before.packetsLost) && Number.isFinite(after.packetsLost) ? Math.max(0, after.packetsLost - before.packetsLost) : null
  return {
    width: playback.width, height: playback.height, codec, sample_seconds: seconds,
    receive_fps: frames === null ? null : frames / seconds,
    display_fps: total === null || dropped === null || elapsed <= 0 ? null : Math.max(0, total - dropped) / elapsed,
    bitrate_mbps: bytes === null ? null : bytes * 8 / seconds / 1e6,
    decode_ms: decoded && decode !== null ? decode * 1000 / decoded : null,
    buffer_ms: emitted && buffer !== null ? buffer * 1000 / emitted : null,
    rtt_ms: rtt !== undefined && Number.isFinite(rtt) && rtt >= 0 ? rtt * 1000 : null,
    packets_lost: loss, frames_dropped: delta(before.framesDropped, after.framesDropped),
  }
}
