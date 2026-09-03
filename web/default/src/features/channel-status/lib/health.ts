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
import { getSuccessRateLevel } from '@/features/performance-metrics/lib/format'

import type { ChannelHealth } from '../types'

export function getChannelHealth(
  requestCount: number,
  successRate: number
): ChannelHealth {
  if (!Number.isFinite(requestCount) || requestCount <= 0) return 'no_data'
  const level = getSuccessRateLevel(successRate)
  if (level === 'excellent' || level === 'good') return 'running'
  if (level === 'warning') return 'fluctuating'
  return 'abnormal'
}

export function formatGroupRatio(ratio: number | string): string {
  if (typeof ratio === 'string') return ratio
  if (!Number.isFinite(ratio)) return '—'
  return `×${ratio}`
}

const OPENAI_HEALTH_RANK: Record<ChannelHealth, number> = {
  no_data: 0,
  running: 1,
  fluctuating: 2,
  abnormal: 3,
}

export function getOpenAIComponentHealth(
  status: string,
  available: boolean
): ChannelHealth {
  if (!available) return 'no_data'
  if (status === 'operational') return 'running'
  if (status === 'degraded_performance' || status === 'under_maintenance') {
    return 'fluctuating'
  }
  if (
    status === 'partial_outage' ||
    status === 'major_outage' ||
    status === 'full_outage'
  ) {
    return 'abnormal'
  }
  return 'no_data'
}

export function getOpenAIGroupHealth(
  statuses: string[],
  available: boolean
): ChannelHealth {
  if (!available) return 'no_data'
  let worst: ChannelHealth = 'running'
  for (const status of statuses) {
    const health = getOpenAIComponentHealth(status, true)
    if (OPENAI_HEALTH_RANK[health] > OPENAI_HEALTH_RANK[worst]) {
      worst = health
    }
  }
  return worst
}
