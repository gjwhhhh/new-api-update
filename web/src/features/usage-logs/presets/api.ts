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
import { requireServerSuccess } from '@/lib/server-error-message'

export type PresetScope = 'self' | 'all'
export type PresetFilters = Partial<
  Record<
    | 'model'
    | 'group'
    | 'type'
    | 'token'
    | 'username'
    | 'excludeUsername'
    | 'channel'
    | 'requestId'
    | 'upstreamRequestId',
    string
  >
>
export type LogFilterPreset = {
  id: number
  name: string
  category: 'common'
  scope: PresetScope
  filters: PresetFilters
  version: number
  updated_at: number
  ignored_fields?: string[]
}
export type PresetPreference = {
  default_preset_id: number | null
  version: number
}
export type PresetCollection = {
  items: LogFilterPreset[]
  preference: PresetPreference
}
type Response<T> = { success: boolean; data: T; code?: string }
const base = '/api/user/self/log-filter-presets'

export async function readPresets(
  scope: PresetScope,
  signal?: AbortSignal
): Promise<PresetCollection> {
  const result = await api.get<Response<PresetCollection>>(base, {
    params: { category: 'common', scope },
    signal,
    disableDuplicate: true,
  })
  return requireServerSuccess(result.data).data
}
export async function readPreset(
  scope: PresetScope,
  id: number,
  signal?: AbortSignal
): Promise<LogFilterPreset> {
  const result = await api.get<Response<LogFilterPreset>>(`${base}/${id}`, {
    params: { category: 'common', scope },
    signal,
    disableDuplicate: true,
  })
  return requireServerSuccess(result.data).data
}
export async function createPreset(
  scope: PresetScope,
  name: string,
  filters: PresetFilters
): Promise<LogFilterPreset> {
  const result = await api.post<Response<LogFilterPreset>>(
    base,
    { name, filters },
    { params: { category: 'common', scope } }
  )
  return requireServerSuccess(result.data).data
}
export async function updatePreset(
  scope: PresetScope,
  id: number,
  payload: { version: number; name?: string; filters?: PresetFilters }
): Promise<LogFilterPreset> {
  const result = await api.patch<Response<LogFilterPreset>>(
    `${base}/${id}`,
    payload,
    { params: { category: 'common', scope } }
  )
  return requireServerSuccess(result.data).data
}
export async function deletePreset(
  scope: PresetScope,
  id: number,
  version: number
): Promise<PresetPreference> {
  const result = await api.delete<Response<PresetPreference>>(`${base}/${id}`, {
    params: { category: 'common', scope },
    data: { version },
  })
  return requireServerSuccess(result.data).data
}
export async function setDefaultPreset(
  scope: PresetScope,
  id: number | null,
  version: number
): Promise<PresetPreference> {
  const result = await api.put<Response<PresetPreference>>(
    '/api/user/self/log-filter-default',
    { default_preset_id: id, version },
    { params: { category: 'common', scope } }
  )
  return requireServerSuccess(result.data).data
}
