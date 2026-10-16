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

export type UserSessionAffinity = {
  user_id: number
  username: string
  disabled: boolean
  version: number
}

export type UserSessionAffinityConfig = UserSessionAffinity & {
  global_enabled: boolean
}

type Response<T> = { success: boolean; data: T; message?: string }

export const userSessionAffinityQueryKey = ['user-session-affinity'] as const

export async function getUserSessionAffinity(
  userId: number,
  signal?: AbortSignal
) {
  const response = await api.get<Response<UserSessionAffinityConfig>>(
    `/api/user/${userId}/session-affinity`,
    // React Query owns deduplication and cancellation for this policy query.
    // A post-save refresh must not reuse an older Axios request.
    { signal, disableDuplicate: true }
  )
  return requireServerSuccess(response.data).data
}

export async function updateUserSessionAffinity(
  userId: number,
  policy: Pick<UserSessionAffinity, 'disabled' | 'version'>
) {
  const response = await api.patch<Response<UserSessionAffinityConfig>>(
    `/api/user/${userId}/session-affinity`,
    policy
  )
  return requireServerSuccess(response.data).data
}

export async function searchUserSessionAffinities(
  params: {
    keyword: string
    disabled_only: boolean
    p: number
    page_size: number
  },
  signal?: AbortSignal
) {
  const response = await api.get<
    Response<{ items: UserSessionAffinity[]; total: number }>
  >('/api/user/session-affinity', { params, signal, disableDuplicate: true })
  return requireServerSuccess(response.data).data
}
