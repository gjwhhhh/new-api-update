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
import { describe, expect, it } from 'vitest'

import type { UpstreamCacheUsageStats, UpstreamCacheWindowStats } from '../../types'
import {
  formatRequestHitRate,
  formatTokenHitRate,
  indexUpstreamCacheByChannel,
  indexUpstreamCacheByGroup,
} from '../upstream-cache'

const sampleWindow = (
  overrides: Partial<UpstreamCacheWindowStats> = {}
): UpstreamCacheWindowStats => ({
  hit: 8,
  total: 10,
  request_hit_rate: 0.8,
  token_hit_rate: 0.35,
  window_seconds: 3600,
  prompt_tokens: 100,
  completion_tokens: 20,
  total_tokens: 120,
  cached_tokens: 35,
  prompt_cache_hit_tokens: 0,
  last_seen_at: 1,
  ...overrides,
})

describe('formatRequestHitRate', () => {
  it('returns dash when total is zero', () => {
    expect(formatRequestHitRate(sampleWindow({ total: 0, request_hit_rate: null }))).toBe(
      '—'
    )
  })

  it('formats finite request hit rate as percent', () => {
    expect(formatRequestHitRate(sampleWindow())).toBe('80.00%')
  })
})

describe('formatTokenHitRate', () => {
  it('returns dash when token rate is missing', () => {
    expect(formatTokenHitRate(sampleWindow({ token_hit_rate: null }))).toBe('—')
  })

  it('formats finite token hit rate as percent', () => {
    expect(formatTokenHitRate(sampleWindow())).toBe('35.00%')
  })
})

describe('indexUpstreamCacheByGroup/Channel', () => {
  it('indexes batch rows by group and channel id', () => {
    const rows: UpstreamCacheUsageStats[] = [
      {
        scope: 'group',
        group: 'vip',
        windows: { '1h': sampleWindow() },
      },
      {
        scope: 'channel',
        channel_id: 12,
        windows: { '24h': sampleWindow({ window_seconds: 86400 }) },
      },
    ]
    expect(indexUpstreamCacheByGroup(rows).get('vip')?.group).toBe('vip')
    expect(indexUpstreamCacheByChannel(rows).get(12)?.channel_id).toBe(12)
  })
})
