/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { GroupBucketPoint } from '../types'

export type AvailabilitySlot = {
  ts: number
  spanSeconds: number
  requestCount: number
  successRate: number | null
  avgLatencyMs: number
}

export function buildAvailabilitySlots(
  series: GroupBucketPoint[]
): AvailabilitySlot[] {
  return series.map((point, index) => {
    const nextTs = series[index + 1]?.ts
    const fallbackSpan =
      nextTs != null && nextTs > point.ts ? nextTs - point.ts : 3600
    const spanSeconds =
      Number.isFinite(point.span_seconds) && point.span_seconds > 0
        ? point.span_seconds
        : fallbackSpan

    return {
      ts: point.ts,
      spanSeconds,
      requestCount: point.request_count,
      successRate: point.success_rate,
      avgLatencyMs: point.avg_latency_ms,
    }
  })
}
