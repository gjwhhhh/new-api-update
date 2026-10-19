import { getDefaultTimeRange } from '../lib/utils'
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
import type { CommonLogFilters } from '../types'
import type { PresetFilters, PresetScope } from './api'

export const presetFilterKeys = [
  'model',
  'group',
  'token',
  'username',
  'excludeUsername',
  'channel',
  'requestId',
  'upstreamRequestId',
] as const
export type LogType = '0' | '1' | '2' | '3' | '4' | '5' | '6' | '7'
export type LogFilterSearch = Partial<
  Record<(typeof presetFilterKeys)[number], string>
> & {
  type?: LogType[]
  startTime?: number
  endTime?: number
  page?: number
  pageSize?: number
  preset?: string
}
export type LogFilterDraft = { filters: CommonLogFilters; logType: LogType }

export function logTypeValue(value: unknown): LogType {
  const item = Array.isArray(value) && value.length === 1 ? value[0] : value
  return typeof item === 'string' && /^[0-7]$/.test(item)
    ? (item as LogType)
    : '0'
}

export function savedFilters(
  draft: LogFilterDraft,
  scope: PresetScope
): PresetFilters {
  const result: PresetFilters = { type: draft.logType }
  for (const key of presetFilterKeys) {
    if (
      scope === 'self' &&
      ['username', 'excludeUsername', 'channel'].includes(key)
    ) {
      continue
    }
    const value = draft.filters[key]?.trim()
    if (value) result[key] = value
  }
  return result
}

export function draftFromSearch(
  search: LogFilterSearch,
  scope: PresetScope
): LogFilterDraft {
  const range = getDefaultTimeRange()
  const filters: CommonLogFilters = {
    startTime:
      search.startTime === undefined ? range.start : new Date(search.startTime),
    endTime:
      search.endTime === undefined ? range.end : new Date(search.endTime),
  }
  for (const key of presetFilterKeys) {
    if (
      scope === 'self' &&
      ['username', 'excludeUsername', 'channel'].includes(key)
    ) {
      continue
    }
    if (search[key]) filters[key] = search[key]
  }
  return { filters, logType: logTypeValue(search.type) }
}

export function draftFromPreset(
  filters: PresetFilters,
  scope: PresetScope
): LogFilterDraft {
  return draftFromSearch(
    { ...filters, type: [logTypeValue(filters.type)] },
    scope
  )
}

export function draftSearch(
  draft: LogFilterDraft,
  selected: string,
  scope: PresetScope
): LogFilterSearch {
  return {
    ...savedFilters(draft, scope),
    type: [draft.logType],
    startTime: draft.filters.startTime?.getTime(),
    endTime: draft.filters.endTime?.getTime(),
    page: 1,
    preset: selected,
  }
}

export function hasExplicitLogFilters(search: LogFilterSearch): boolean {
  return (
    presetFilterKeys.some((key) => Boolean(search[key])) ||
    search.startTime !== undefined ||
    search.endTime !== undefined ||
    Boolean(search.type?.length)
  )
}

export function logFilterSearchKey(search: LogFilterSearch): string {
  return JSON.stringify([
    search.preset ?? '',
    search.startTime ?? null,
    search.endTime ?? null,
    search.page ?? 1,
    search.pageSize ?? null,
    search.type ?? [],
    ...presetFilterKeys.map((key) => search[key] ?? ''),
  ])
}

export function samePresetFilters(a: PresetFilters, b: PresetFilters): boolean {
  return (
    logTypeValue(a.type) === logTypeValue(b.type) &&
    presetFilterKeys.every(
      (key) => (a[key]?.trim() ?? '') === (b[key]?.trim() ?? '')
    )
  )
}
