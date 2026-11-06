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

import type {
  MailBatchDetail,
  MailBatchSummary,
  MailConfiguration,
  MailDetail,
  MailPage,
  MailPreview,
  MailPreviewRequest,
  MailRecipient,
  MailRecord,
  MailSettings,
} from './types'

interface MailResponse<T> {
  success: boolean
  message: string
  data: T
}
async function getMail<T>(
  path: string,
  params?: Record<string, unknown>
): Promise<T> {
  const response = await api.get<MailResponse<T>>(`/api/mail${path}`, {
    params,
  })
  return requireServerSuccess(response.data).data
}
async function postMail<T>(path: string, body: unknown = {}): Promise<T> {
  const response = await api.post<MailResponse<T>>(`/api/mail${path}`, body)
  return requireServerSuccess(response.data).data
}
export const getMailConfiguration = () =>
  getMail<MailConfiguration>('/settings')
export const getMailRecords = (params: Record<string, unknown>) =>
  getMail<MailPage<MailRecord>>('/records', params)
export const getMailDetail = (id: number) =>
  getMail<MailDetail>(`/records/${id}`)
export const getMailRecipients = (params: Record<string, unknown>) =>
  getMail<MailPage<MailRecipient>>('/recipients', params)
export const previewMail = (body: MailPreviewRequest) =>
  postMail<MailPreview>('/batches/preview', body)
export const submitMail = (id: number) =>
  postMail<null>(`/batches/${id}/submit`)
export const testMail = (batchId: number, userId: number) =>
  postMail<null>('/test', { batch_id: batchId, user_id: userId })
export const getMailBatches = (params: Record<string, unknown>) =>
  getMail<MailPage<MailBatchSummary>>('/batches', params)
export const getMailBatch = (id: number, params: Record<string, unknown>) =>
  getMail<MailBatchDetail>(`/batches/${id}`, params)
export const operateMailBatch = (id: number, action: 'retry' | 'cancel') =>
  postMail<null>(`/batches/${id}/${action}`)
export async function updateMailSettings(
  settings: MailSettings
): Promise<void> {
  const response = await api.put<MailResponse<null>>(
    '/api/mail/settings',
    settings
  )
  requireServerSuccess(response.data)
}
