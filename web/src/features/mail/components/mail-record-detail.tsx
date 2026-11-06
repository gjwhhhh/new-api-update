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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { RichContent } from '@/components/rich-content'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { formatTimestampToDate } from '@/lib/format'

import { getMailDetail } from '../api'
import type { MailAttempt } from '../types'
import { MailStatus, MailError } from './mail-status'

export function MailRecordDetail(props: {
  id: number | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['mail', 'record', props.id],
    queryFn: () => {
      if (props.id === null) throw new Error(t('Invalid email ID'))
      return getMailDetail(props.id)
    },
    enabled: props.id !== null,
  })
  return (
    <Dialog
      open={props.id !== null}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={query.data?.message.subject ?? t('Email details')}
      contentClassName='sm:max-w-4xl'
    >
      {query.isPending ? <LoadingState /> : null}
      {query.isError ? (
        <ErrorState onRetry={() => void query.refetch()} />
      ) : null}
      {query.data ? (
        <div className='min-w-0 space-y-4'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='break-all'>{query.data.record.email}</span>
            <MailStatus status={query.data.record.status} />
          </div>
          {query.data.message.redacted ? (
            <Alert>
              <AlertDescription>
                {t(
                  'Security codes and authentication links are redacted before storage.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}
          <RichContent
            mode='html'
            content={query.data.message.body}
            className='rounded-md border p-4 break-words'
          />
          <StaticDataTable<MailAttempt>
            data={query.data.attempts}
            getRowKey={(row) => row.id}
            columns={[
              {
                id: 'time',
                header: t('Time'),
                cell: (row) => formatTimestampToDate(row.started_at),
              },
              {
                id: 'status',
                header: t('Status'),
                cell: (row) => <MailStatus status={row.status} />,
              },
              {
                id: 'stage',
                header: t('SMTP stage'),
                cell: (row) => row.stage,
              },
              {
                id: 'error',
                header: t('Error'),
                cell: (row) => <MailError message={row.error} />,
              },
              {
                id: 'message',
                header: 'Message-ID',
                cell: (row) => (
                  <span className='break-all'>{row.message_id || '-'}</span>
                ),
              },
            ]}
          />
        </div>
      ) : null}
    </Dialog>
  )
}
