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

import type {
  ChannelMetricsSort,
  ChannelStatusData,
  ChannelStatusDetailData,
  GroupHours,
  GroupsStatusData,
  GroupSortMode,
  OpenAIStatusData,
} from './types'

export async function getPerfMetricGroups(
  hours: GroupHours,
  sort: GroupSortMode = 'custom'
) {
  const res = await api.get<{
    success: boolean
    data: GroupsStatusData
    message?: string
  }>('/api/perf-metrics/groups', {
    params: { hours, sort },
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to load group status')
  }
  return res.data.data
}

export async function getPerfMetricChannels(params: {
  hours: GroupHours
  page: number
  pageSize: number
  search: string
  group: string
  health: string
  channelStatus: '' | 'enabled' | 'auto_disabled' | 'manually_disabled'
  providerType: number | null
  sort: ChannelMetricsSort
  order: 'asc' | 'desc'
}) {
  const res = await api.get<{
    success: boolean
    data: ChannelStatusData
    message?: string
  }>('/api/channel/status-metrics', {
    params: {
      hours: params.hours,
      p: params.page,
      page_size: params.pageSize,
      search: params.search || undefined,
      group: params.group || undefined,
      health: params.health || undefined,
      channel_status: params.channelStatus || undefined,
      type: params.providerType ?? undefined,
      sort: params.sort,
      order: params.order,
    },
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to load channel status')
  }
  return res.data.data
}

export async function getPerfMetricChannelDetail(
  channelId: number,
  hours: GroupHours
) {
  const res = await api.get<{
    success: boolean
    data: ChannelStatusDetailData
    message?: string
  }>(`/api/channel/${channelId}/status-metrics`, {
    params: { hours },
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Failed to load channel status')
  }
  return res.data.data
}

export async function clearPerfMetricChannelSamples(
  channelId: number,
  hours: GroupHours
) {
  const res = await api.post<{
    success: boolean
    message?: string
    data?: {
      channel_id: number
      hours: number
      start_ts: number
      end_ts: number
    }
  }>(`/api/channel/${channelId}/status-metrics/clear`, { hours })
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to clear recent samples')
  }
  return res.data.data
}

export async function clearPerfMetricGroupSamples(
  group: string,
  hours: GroupHours
) {
  const res = await api.post<{
    success: boolean
    message?: string
    data?: {
      group: string
      hours: number
      start_ts: number
      end_ts: number
    }
  }>('/api/perf-metrics/groups/clear', { group, hours })
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to clear recent samples')
  }
  return res.data.data
}

export async function updatePerfMetricGroupVisibility(
  group: string,
  visibleToUsers: boolean
) {
  const res = await api.put<{
    success: boolean
    message?: string
    data?: {
      group: string
      visible_to_users: boolean
    }
  }>('/api/perf-metrics/groups/visibility', {
    group,
    visible_to_users: visibleToUsers,
  })
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update group visibility')
  }
  return res.data.data
}

export async function updatePerfMetricGroupDisplayOrder(groups: string[]) {
  const res = await api.put<{
    success: boolean
    message?: string
    data?: {
      groups: string[]
    }
  }>('/api/perf-metrics/groups/display-order', { groups })
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to save display order')
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
