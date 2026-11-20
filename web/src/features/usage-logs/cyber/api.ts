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
import type { ApiResponse } from '@/features/profile/types'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type CyberPolicy = {
  mode: 'off' | 'shadow' | 'enforce'
  scope: 'all' | 'token' | 'channel'
  token_ids: number[] | null
  channel_ids: number[] | null
  ttl_seconds: number
  match_message: boolean
}

export type CyberEvent = {
  created_at: number
  request_id: string
  channel_id: number
  token_id: number
  token_name: string
  error_code: string
  protocol: string
  source: string
  action: string
  reason?: string
  fingerprint?: string
}

export type CyberBan = {
  token_id: number
  channel_id: number
  fingerprint: string
  generation: string
  request_id: string
  created_at: number
  expires_at?: number
  permanent: boolean
}

type CyberReads = {
  policy: CyberPolicy
  events: { items: CyberEvent[]; total: number }
  bans: { items: CyberBan[]; cursor: string }
}

export async function fetchCyber<K extends keyof CyberReads>(
  path: K,
  params?: Record<string, string | number>,
  signal?: AbortSignal
): Promise<CyberReads[K]> {
  const response = await api.get<ApiResponse<CyberReads[K]>>(
    `/api/cyber/${path}`,
    { params, signal }
  )
  const result = requireServerSuccess(response.data).data
  if (!result) throw new Error('Missing Cyber API response')
  return result
}

export async function saveCyberPolicy(
  policy: CyberPolicy
): Promise<CyberPolicy> {
  const response = await api.put<ApiResponse<CyberPolicy>>(
    '/api/cyber/policy',
    policy
  )
  const result = requireServerSuccess(response.data).data
  if (!result) throw new Error('Missing Cyber API response')
  return result
}

export async function revokeCyberBan(
  ban: CyberBan,
  reason: string
): Promise<void> {
  const { token_id, channel_id, fingerprint, generation } = ban
  const response = await api.post<ApiResponse<null>>('/api/cyber/bans/revoke', {
    token_id,
    channel_id,
    fingerprint,
    generation,
    reason,
  })
  requireServerSuccess(response.data)
}
