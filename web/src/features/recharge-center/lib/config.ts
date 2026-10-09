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
*/
import type {
  RechargeCenterConfig,
  RechargeCenterConfigResponse,
  RechargeCenterDisplayMode,
} from '../types'

function isDisplayMode(value: unknown): value is RechargeCenterDisplayMode {
  return value === 'redirect' || value === 'embed'
}

export function isValidRechargeCenterURL(value: string): boolean {
  const trimmed = value.trim()
  if (!trimmed || trimmed.length > 2048) return false

  try {
    const url = new URL(trimmed)
    return (
      url.protocol === 'https:' &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password
    )
  } catch {
    return false
  }
}

export function parseRechargeCenterConfig(
  response: RechargeCenterConfigResponse
): RechargeCenterConfig | null {
  const data = response.data
  if (!response.success || !data?.enabled || typeof data.url !== 'string') {
    return null
  }

  const url = data.url.trim()
  if (!isValidRechargeCenterURL(url) || !isDisplayMode(data.display_mode)) {
    return null
  }

  return {
    url,
    displayMode: data.display_mode,
  }
}
