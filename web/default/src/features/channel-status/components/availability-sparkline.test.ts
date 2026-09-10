import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

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

    assert.equal(slots.length, 48)
    assert.deepEqual(slots[0], {
      ts: 0,
      spanSeconds: 3600,
      requestCount: 10,
      successRate: 100,
      avgLatencyMs: 0,
    })
    assert.deepEqual(slots.at(-1), {
      ts: 47 * 3600,
      spanSeconds: 3600,
      requestCount: 10,
      successRate: 100,
      avgLatencyMs: 0,
    })
    assert.notEqual(slots[0]?.ts, slots[1]?.ts)
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

    assert.deepEqual(slots, [
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
