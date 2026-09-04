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
import type { ChannelHealth, GroupHours, GroupSortMode } from './types'

export const GROUP_HOURS_OPTIONS: GroupHours[] = [24, 168]

export const GROUP_SORT_OPTIONS: GroupSortMode[] = ['custom', 'traffic']

export const CHANNEL_HEALTH_FILTERS: ChannelHealth[] = [
  'running',
  'fluctuating',
  'abnormal',
  'no_data',
]

export const CHANNEL_HEALTH_LABEL: Record<ChannelHealth, string> = {
  running: 'Running',
  fluctuating: 'Fluctuating',
  abnormal: 'Abnormal',
  no_data: 'No data',
}

export const OPENAI_COMPONENT_STATUS_LABEL: Record<string, string> = {
  operational: 'Operational',
  degraded_performance: 'Degraded',
  under_maintenance: 'Maintenance',
  partial_outage: 'Partial outage',
  major_outage: 'Major outage',
  full_outage: 'Major outage',
}

export const CHANNEL_STATUS_QUERY_KEYS = {
  groups: (hours: number, sort: GroupSortMode = 'custom') =>
    ['channel-status', 'groups', hours, sort] as const,
  openai: ['channel-status', 'openai'] as const,
}
