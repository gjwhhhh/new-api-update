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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import { MAIL_STATUS_LABELS } from '../constants'
import type { MailStatus as Status } from '../types'

export function MailStatus(props: { status: Status }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant={
        props.status === 'failed' || props.status === 'unknown'
          ? 'destructive'
          : 'secondary'
      }
    >
      {t(MAIL_STATUS_LABELS[props.status] ?? props.status)}
    </Badge>
  )
}

export function MailError(props: { message: string }) {
  const { t } = useTranslation()
  const smtpError = /^SMTP (\w+) failed \(code (\d+)\)$/.exec(props.message)
  if (smtpError) {
    return t('SMTP {{stage}} failed (code {{code}})', {
      stage: smtpError[1],
      code: smtpError[2],
    })
  }
  return props.message ? t(props.message) : '-'
}
