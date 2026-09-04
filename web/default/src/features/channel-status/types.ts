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
export type GroupBucketPoint = {
  ts: number
  avg_ttft_ms: number
  avg_latency_ms: number
  success_rate: number | null
  avg_tps: number
  request_count: number
}

export type GroupModelStat = {
  model_name: string
  request_count: number
  success_count: number
  success_rate: number
  avg_ttft_ms: number
  avg_latency_ms: number
  avg_tps: number
}

export type GroupStatusItem = {
  group: string
  name: string
  description: string
  ratio: number | string
  request_count: number
  success_count: number
  success_rate: number
  avg_ttft_ms: number
  avg_latency_ms: number
  avg_tps: number
  series: GroupBucketPoint[]
  models: GroupModelStat[]
  visible_to_users?: boolean
}

export type GroupsStatusData = {
  bucket_seconds: number
  start_ts: number
  end_ts: number
  sort?: GroupSortMode
  display_order?: string[]
  groups: GroupStatusItem[]
}

export type OpenAIUptimeEvent = {
  name: string
  impact_status: string
  component_names?: string[]
  incident_status?: string
  url?: string
}

export type OpenAIUptimeDay = {
  date: string
  status: string
  events?: OpenAIUptimeEvent[]
}

export type OpenAIUptimeHour = {
  ts: string
  status: string
  events?: OpenAIUptimeEvent[]
}

export type OpenAIStatusComponent = {
  id: string
  name: string
  status: string
  uptime_percent?: number | null
  series?: OpenAIUptimeDay[]
  hourly_series?: OpenAIUptimeHour[]
}

export type OpenAIStatusGroup = {
  name: string
  uptime_percent?: number | null
  uptime_days?: number
  series?: OpenAIUptimeDay[]
  hourly_hours?: number
  hourly_series?: OpenAIUptimeHour[]
  components: OpenAIStatusComponent[]
}

export type OpenAIStatusIncident = {
  id: string
  name: string
  status: string
  impact: string
  affected_components: string[]
  affected_groups: string[]
  updated_at: string
  url: string
}

export type OpenAIStatusData = {
  available: boolean
  indicator: string
  description: string
  updated_at: string
  source_url: string
  groups: OpenAIStatusGroup[]
  incidents: OpenAIStatusIncident[]
  history: OpenAIStatusIncident[]
}

export type ChannelHealth = 'running' | 'fluctuating' | 'abnormal' | 'no_data'
export type ChannelStatusTab = 'local' | 'openai'
export type GroupHours = 24 | 168
export type GroupSortMode = 'custom' | 'traffic'
