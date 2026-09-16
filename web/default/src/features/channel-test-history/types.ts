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
export type ChannelTestResultStatus = 'succeeded' | 'failed' | 'cancelled'

export type ChannelTestHistoryTimeRange =
  | '24h'
  | '7d'
  | '30d'
  | 'all'
  | 'custom'

export type ChannelTestResult = {
  id: number
  run_id: string
  request_id: string
  task_id?: string
  channel_id: number
  channel_name: string
  channel_type: number
  source: 'scheduled' | 'manual_batch' | 'manual_single'
  health_check_mode?: string
  model_name: string
  endpoint_type?: string
  request_path: string
  is_stream: boolean
  status: ChannelTestResultStatus
  upstream_http_status: number
  result_status_code: number
  failure_kind: string
  key_index?: number
  state_action: string
  duration_ms: number
  created_at: number
}

export type ChannelTestHistorySummary = {
  tested: number
  succeeded: number
  failed: number
  cancelled: number
}

export type ChannelTestRun = {
  task_id: string
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  processed: number
  total: number
  result?: {
    tested: number
    succeeded: number
    failed: number
    disabled: number
    enabled: number
  }
}

export type ChannelTestHistoryData = {
  items: ChannelTestResult[]
  total: number
  summary: ChannelTestHistorySummary
  recording_enabled: boolean
  run: ChannelTestRun | null
}

export type ChannelTestHistoryParams = {
  p: number
  page_size: number
  channel_id?: number
  run_id?: string
  task_id?: string
  status?: ChannelTestResultStatus
  source?: ChannelTestResult['source']
  model_name?: string
  start_at?: number
  end_at?: number
}

export type ChannelTestHistoryResponse = {
  success: boolean
  message?: string
  data?: ChannelTestHistoryData
}
