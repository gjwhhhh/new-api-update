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
import { createContext, useContext } from 'react'

import type { CommonLogFilters } from '../types'
import type { LogFilterPreset, PresetCollection, PresetScope } from './api'
import type { LogFilterDraft, LogFilterSearch, LogType } from './filters'

export interface LogPresetContextValue {
  draft: LogFilterDraft
  applied: LogFilterSearch
  ready: boolean
  busy: boolean
  dialogOpen: boolean
  loading: boolean
  error: unknown
  collection?: PresetCollection
  selected: string
  baseline: LogFilterPreset | null
  dirty: boolean
  updated: boolean
  notice: 'unavailable' | 'ignored' | null
  change: (
    field: keyof CommonLogFilters,
    value: Date | string | undefined
  ) => void
  changeType: (value: LogType) => void
  apply: (filters?: CommonLogFilters) => Promise<boolean>
  reset: () => Promise<boolean>
  select: (id: string) => void
  save: () => void
  saveAs: () => void
  rename: () => void
  remove: () => void
  setDefault: () => void
  changeScope: (scope: PresetScope) => void
  retry: () => void
  useSystemDefault: () => void
}

export const LogPresetContext = createContext<LogPresetContextValue | null>(
  null
)
export function useLogPresets() {
  return useContext(LogPresetContext)
}
