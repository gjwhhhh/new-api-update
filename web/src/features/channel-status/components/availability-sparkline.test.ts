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
import { describe, expect, test } from 'vitest'

import { buildAvailabilitySlots } from '../lib/availability-slots'

describe('availability slots', () => {
  test('preserves the distinct server-provided time ranges', () => {
    const series = Array.from({ length: 48 }, (_, index) => ({
      ts: index * 3600,
      span_seconds: 3600,
      avg_ttft_ms: 0,
      avg_latency_ms: 0,
      success_rate: index === 1 ? 80 : 100,
      avg_tps: 0,
      request_count: 10,
    }))

    const slots = buildAvailabilitySlots(series)

    expect(slots).toHaveLength(48)
    expect(slots[0]).toEqual({
      ts: 0,
      spanSeconds: 3600,
      requestCount: 10,
      successRate: 100,
      avgLatencyMs: 0,
    })
    expect(slots.at(-1)).toEqual({
      ts: 47 * 3600,
      spanSeconds: 3600,
      requestCount: 10,
      successRate: 100,
      avgLatencyMs: 0,
    })
    expect(slots[0]?.ts).not.toBe(slots[1]?.ts)
  })

  test('retains server aggregation spans for seven-day data', () => {
    const slots = buildAvailabilitySlots([
      {
        ts: 0,
        span_seconds: 3 * 3600,
        avg_ttft_ms: 0,
        avg_latency_ms: 0,
        success_rate: null,
        avg_tps: 0,
        request_count: 0,
      },
      {
        ts: 3 * 3600,
        span_seconds: 4 * 3600,
        avg_ttft_ms: 0,
        avg_latency_ms: 0,
        success_rate: 100,
        avg_tps: 0,
        request_count: 2,
      },
    ])

    expect(slots).toEqual([
      {
        ts: 0,
        spanSeconds: 3 * 3600,
        requestCount: 0,
        successRate: null,
        avgLatencyMs: 0,
      },
      {
        ts: 3 * 3600,
        spanSeconds: 4 * 3600,
        requestCount: 2,
        successRate: 100,
        avgLatencyMs: 0,
      },
    ])
  })
})
