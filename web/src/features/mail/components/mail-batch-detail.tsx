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
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getMailBatch } from '../api'
import { MAIL_ACTIVE_STATUSES, MAIL_STATUS_LABELS } from '../constants'
import type { MailRecord } from '../types'
import { MailRecordDetail } from './mail-record-detail'
import { MailStatus, MailError } from './mail-status'

const EMPTY: MailRecord[] = []
export function MailBatchDetail(props: { id: number; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [record, setRecord] = useState<number | null>(null)
  const query = useQuery({
    queryKey: ['mail', 'batch', props.id, pagination],
    queryFn: () =>
      getMailBatch(props.id, {
        offset: pagination.pageIndex * pagination.pageSize,
        limit: pagination.pageSize,
      }),
    refetchInterval: (state) =>
      state.state.data?.counts.some(
        (count) =>
          MAIL_ACTIVE_STATUSES.includes(count.status) && count.count > 0
      )
        ? 3000
        : false,
  })
  const columns: ColumnDef<MailRecord, unknown>[] = [
    {
      accessorKey: 'email',
      header: t('Recipient'),
      cell: ({ row }) => (
        <Button
          className='h-auto px-0 text-start break-all whitespace-normal'
          variant='link'
          onClick={() => setRecord(row.original.id)}
        >
          {row.original.email}
        </Button>
      ),
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => <MailStatus status={row.original.status} />,
    },
    {
      accessorKey: 'attempts',
      header: t('Attempts'),
      cell: ({ row }) => formatNumber(row.original.attempts, locale),
    },
    {
      accessorKey: 'last_error',
      header: t('Error'),
      cell: ({ row }) => <MailError message={row.original.last_error} />,
    },
  ]
  const { table } = useDataTable({
    data: query.data?.items ?? EMPTY,
    columns,
    totalCount: query.data?.total ?? 0,
    manualPagination: true,
    pagination,
    onPaginationChange: setPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) props.onClose()
        }}
        title={query.data?.message.subject ?? t('Email batch')}
        contentClassName='sm:max-w-5xl'
      >
        {query.isError ? (
          <ErrorState onRetry={() => void query.refetch()} />
        ) : (
          <div className='space-y-4'>
            <div className='flex flex-wrap gap-3'>
              {query.data?.counts.map((count) => (
                <span key={count.status}>
                  {t(MAIL_STATUS_LABELS[count.status])}:{' '}
                  {formatNumber(count.count, locale)}
                </span>
              ))}
            </div>
            {query.data ? (
              <RichContent
                mode='html'
                content={query.data.message.body}
                className='rounded-md border p-3 break-words'
              />
            ) : null}
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={query.isPending}
              toolbarProps={null}
              paginationInFooter={false}
              compactPagination
            />
          </div>
        )}
      </Dialog>
      <MailRecordDetail id={record} onClose={() => setRecord(null)} />
    </>
  )
}
