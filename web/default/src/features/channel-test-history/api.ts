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
  ChannelTestHistoryParams,
  ChannelTestHistoryResponse,
  ChannelTestResult,
} from './types'

export async function getChannelTestHistory(
  params: ChannelTestHistoryParams
): Promise<ChannelTestHistoryResponse> {
  const response = await api.get('/api/channel/test-history', { params })
  return response.data
}

export async function getChannelTestResult(
  id: number
): Promise<{ success: boolean; message?: string; data?: ChannelTestResult }> {
  const response = await api.get(`/api/channel/test-history/${id}`)
  return response.data
}
