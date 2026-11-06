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
export type MailStatus =
  | 'prepared'
  | 'pending'
  | 'sending'
  | 'retry'
  | 'accepted'
  | 'failed'
  | 'unknown'
  | 'cancelled'
  | 'skipped'
export interface MailMessage {
  id: number
  subject: string
  body: string
  kind: string
  source: string
  redacted: boolean
  created_at: number
}
export interface MailDelivery {
  id: number
  message_id: number
  batch_id: number
  user_id: number
  user_ids: number[]
  username: string
  email: string
  status: MailStatus
  attempts: number
  last_error: string
  created_at: number
  updated_at: number
}
export interface MailRecord extends MailDelivery {
  subject: string
  kind: string
  source: string
  redacted: boolean
}
export interface MailAttempt {
  id: number
  message_id: string
  stage: string
  status: MailStatus
  error: string
  smtp_code: number
  started_at: number
  finished_at: number
}
export interface MailExclusion {
  user_id: number
  email: string
  reason: string
}
export interface MailBatch {
  id: number
  message_id: number
  actor_id: number
  actor_name: string
  status: string
  total: number
  matched: number
  duplicates: number
  exclusions: MailExclusion[]
  expires_at: number
  created_at: number
  updated_at: number
}
export interface MailBatchSummary extends MailBatch {
  subject: string
  counts: Partial<Record<MailStatus, number>>
}
export interface MailRecipient {
  id: number
  username: string
  email: string
  group: string
  status: number
}
export interface RecipientSelection {
  keyword: string
  group: string
  status: number
  ids: number[]
  all: boolean
}
export interface MailPreviewRequest {
  subject: string
  body: string
  selection: RecipientSelection
}
export interface MailPreview {
  batch: MailBatch
  message: MailMessage
  recipients: MailDelivery[]
}
export interface MailPage<T> {
  items: T[]
  total: number
}
export interface MailDetail {
  record: MailRecord
  message: MailMessage
  attempts: MailAttempt[]
}
export interface MailBatchDetail extends MailPage<MailRecord> {
  batch: MailBatch
  message: MailMessage
  counts: { status: MailStatus; count: number }[]
}
export interface MailSettings {
  records_enabled: boolean
  sending_enabled: boolean
  batch_limit: number
  interval_milliseconds: number
  retention_days: number
}
export interface MailConfiguration {
  settings: MailSettings
  smtp_configured: boolean
}
export type MailFilters = {
  keyword: string
  user_id: string
  status: string
  kind: string
  source: string
  start: string
  end: string
}
