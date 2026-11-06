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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { getMailBatches, operateMailBatch } from '../api'
import { MAIL_ACTIVE_STATUSES } from '../constants'
import type { MailBatchSummary } from '../types'
import { MailBatchDetail } from './mail-batch-detail'

const EMPTY: MailBatchSummary[] = []
export function MailBatches(props: {
  canOperate: boolean
  sendingEnabled: boolean
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const [detail, setDetail] = useState<number | null>(null)
  const [operation, setOperation] = useState<{
    id: number
    action: 'retry' | 'cancel'
  } | null>(null)
  const query = useQuery({
    queryKey: ['mail', 'batches', pagination],
    queryFn: () =>
      getMailBatches({
        offset: pagination.pageIndex * pagination.pageSize,
        limit: pagination.pageSize,
      }),
    refetchInterval: (state) =>
      state.state.data?.items.some((batch) =>
        MAIL_ACTIVE_STATUSES.some((status) => (batch.counts[status] ?? 0) > 0)
      )
        ? 3000
        : false,
  })
  const mutation = useMutation({
    mutationFn: () => {
      if (!operation) throw new Error(t('Invalid email request'))
      return operateMailBatch(operation.id, operation.action)
    },
    onSuccess: async () => {
      setOperation(null)
      await client.invalidateQueries({ queryKey: ['mail'] })
    },
    onError: (error) => handleServerError(error),
  })
  const columns: ColumnDef<MailBatchSummary, unknown>[] = [
    {
      accessorKey: 'subject',
      header: t('Subject'),
      cell: ({ row }) => (
        <Button
          variant='link'
          className='h-auto max-w-full px-0 text-start break-words whitespace-normal'
          onClick={() => setDetail(row.original.id)}
        >
          {row.original.subject}
        </Button>
      ),
    },
    { accessorKey: 'actor_name', header: t('Sender') },
    {
      id: 'progress',
      header: t('Progress'),
      cell: ({ row }) => (
        <span>
          {t('{{accepted}} accepted / {{total}} total', {
            accepted: formatNumber(row.original.counts.accepted ?? 0, locale),
            total: formatNumber(row.original.total, locale),
          })}
        </span>
      ),
    },
    {
      id: 'failures',
      header: t('Failed / unknown'),
      cell: ({ row }) =>
        `${formatNumber(row.original.counts.failed ?? 0, locale)} / ${formatNumber(row.original.counts.unknown ?? 0, locale)}`,
    },
    {
      accessorKey: 'created_at',
      header: t('Time'),
      cell: ({ row }) => formatTimestampToDate(row.original.created_at),
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) =>
        props.canOperate ? (
          <div className='flex flex-wrap gap-1'>
            <Button
              variant='outline'
              size='sm'
              disabled={
                !props.sendingEnabled ||
                row.original.status !== 'pending' ||
                !row.original.counts.failed
              }
              onClick={() =>
                setOperation({ id: row.original.id, action: 'retry' })
              }
            >
              {t('Retry failed')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              disabled={
                row.original.status !== 'pending' ||
                !(
                  (row.original.counts.pending ?? 0) +
                  (row.original.counts.retry ?? 0)
                )
              }
              onClick={() =>
                setOperation({ id: row.original.id, action: 'cancel' })
              }
            >
              {t('Cancel pending')}
            </Button>
          </div>
        ) : null,
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
    <section className='min-w-0 space-y-3' aria-label={t('Email batches')}>
      <div className='flex items-center justify-between gap-2'>
        <h3 className='font-medium'>{t('Email batches')}</h3>
        <Button variant='outline' onClick={() => void query.refetch()}>
          {t('Refresh')}
        </Button>
      </div>
      {query.isError ? (
        <ErrorState onRetry={() => void query.refetch()} />
      ) : (
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={query.isPending}
          toolbarProps={null}
          paginationInFooter={false}
          compactPagination
          emptyTitle={t('No email batches')}
        />
      )}
      <ConfirmDialog
        open={operation !== null}
        onOpenChange={(open) => {
          if (!open) setOperation(null)
        }}
        title={t(
          operation?.action === 'retry'
            ? 'Retry failed email?'
            : 'Cancel pending email?'
        )}
        desc={t(
          operation?.action === 'retry'
            ? 'Only confirmed failures are retried. Accepted and unknown deliveries are excluded.'
            : 'Only email that has not started sending will be cancelled.'
        )}
        isLoading={mutation.isPending}
        handleConfirm={() => mutation.mutate()}
      />
      {detail !== null ? (
        <MailBatchDetail
          key={detail}
          id={detail}
          onClose={() => setDetail(null)}
        />
      ) : null}
    </section>
  )
}
