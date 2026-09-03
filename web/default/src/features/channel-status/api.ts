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
import { api } from '@/lib/api'

import type { GroupsStatusData, OpenAIStatusData } from './types'

export async function getPerfMetricGroups(hours: number) {
  const res = await api.get<{
    success: boolean
    data: GroupsStatusData
    message?: string
  }>('/api/perf-metrics/groups', {
    params: { hours },
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to load group status')
  }
  return res.data.data
}

export async function getOpenAIStatus() {
  const res = await api.get<{
    success: boolean
    data: OpenAIStatusData
    message?: string
  }>('/api/openai/status', {
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to load OpenAI status')
  }
  return res.data.data
}
