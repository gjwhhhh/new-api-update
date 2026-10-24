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
import type {
  UpstreamCacheUsageStats,
  UpstreamCacheWindow,
  UpstreamCacheWindowStats,
} from '../types'

export function formatRequestHitRate(
  stats: UpstreamCacheWindowStats | undefined
): string {
  if (!stats || stats.total <= 0 || stats.request_hit_rate == null) {
    return '—'
  }
  if (!Number.isFinite(stats.request_hit_rate)) {
    return '—'
  }
  return `${(stats.request_hit_rate * 100).toFixed(2)}%`
}

export function formatTokenHitRate(
  stats: UpstreamCacheWindowStats | undefined
): string {
  if (!stats || stats.token_hit_rate == null) {
    return '—'
  }
  if (!Number.isFinite(stats.token_hit_rate)) {
    return '—'
  }
  return `${(stats.token_hit_rate * 100).toFixed(2)}%`
}

export function getWindowStats(
  entry: UpstreamCacheUsageStats | undefined,
  window: UpstreamCacheWindow
): UpstreamCacheWindowStats | undefined {
  return entry?.windows?.[window]
}

export function indexUpstreamCacheByGroup(
  items: UpstreamCacheUsageStats[]
): Map<string, UpstreamCacheUsageStats> {
  const map = new Map<string, UpstreamCacheUsageStats>()
  for (const item of items) {
    if (item.group) {
      map.set(item.group, item)
    }
  }
  return map
}

export function indexUpstreamCacheByChannel(
  items: UpstreamCacheUsageStats[]
): Map<number, UpstreamCacheUsageStats> {
  const map = new Map<number, UpstreamCacheUsageStats>()
  for (const item of items) {
    if (item.channel_id && item.channel_id > 0) {
      map.set(item.channel_id, item)
    }
  }
  return map
}
