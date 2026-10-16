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
export type GroupDisplayCategory = {
  id: string
  name: string
  icon?: string
  order: number
}

export type GroupDisplayAssignment = {
  category_id: string
  order: number
}

export type GroupDisplayConfig = {
  categories: GroupDisplayCategory[]
  assignments: Record<string, GroupDisplayAssignment>
}

export const EMPTY_GROUP_DISPLAY_CONFIG: GroupDisplayConfig = {
  categories: [],
  assignments: {},
}

export function parseGroupDisplayConfig(value: string): GroupDisplayConfig {
  try {
    const parsed = JSON.parse(value) as Partial<GroupDisplayConfig>
    return {
      categories: Array.isArray(parsed.categories) ? parsed.categories : [],
      assignments:
        parsed.assignments && typeof parsed.assignments === 'object'
          ? parsed.assignments
          : {},
    }
  } catch {
    return { categories: [], assignments: {} }
  }
}

export function serializeGroupDisplayConfig(
  config: GroupDisplayConfig
): string {
  return JSON.stringify(config, null, 2)
}
