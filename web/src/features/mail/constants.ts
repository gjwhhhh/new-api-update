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
import type { MailStatus } from './types'

export const MAIL_STATUS_LABELS: Record<MailStatus, string> = {
  prepared: 'Ready for confirmation',
  pending: 'Queued',
  sending: 'Sending',
  retry: 'Waiting to retry',
  accepted: 'SMTP accepted',
  failed: 'Failed',
  unknown: 'Outcome unknown',
  cancelled: 'Cancelled',
  skipped: 'Skipped',
}
export const MAIL_KIND_LABELS: Record<string, string> = {
  manual: 'Admin email',
  test: 'Test email',
  verification: 'Email verification',
  password_reset: 'Password reset',
  email_binding: 'Email binding',
  account_security: 'Account security',
  quota_exceed: 'Low quota reminder',
  channel_update: 'Channel update',
  channel_test: 'Channel test',
}
export const MAIL_ACTIVE_STATUSES: MailStatus[] = [
  'pending',
  'sending',
  'retry',
]
