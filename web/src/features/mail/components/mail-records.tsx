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
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatTimestampToDate } from '@/lib/format'

import { getMailRecords } from '../api'
import { MAIL_KIND_LABELS, MAIL_STATUS_LABELS } from '../constants'
import type { MailFilters, MailRecord } from '../types'
import { MailRecordDetail } from './mail-record-detail'
import { MailStatus } from './mail-status'

const EMPTY: MailRecord[] = []
const INITIAL_FILTERS: MailFilters = {
  keyword: '',
  user_id: '',
  status: '',
  kind: '',
  source: '',
  start: '',
  end: '',
}
export function MailRecords() {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(INITIAL_FILTERS)
  const [filters, setFilters] = useState(INITIAL_FILTERS)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [detail, setDetail] = useState<number | null>(null)
  const query = useQuery({
    queryKey: ['mail', 'records', filters, pagination],
    queryFn: () =>
      getMailRecords({
        ...filters,
        start: filters.start
          ? Math.floor(new Date(`${filters.start}T00:00:00`).getTime() / 1000)
          : undefined,
        end: filters.end
          ? Math.floor(new Date(`${filters.end}T23:59:59`).getTime() / 1000)
          : undefined,
        user_id: filters.user_id || undefined,
        offset: pagination.pageIndex * pagination.pageSize,
        limit: pagination.pageSize,
      }),
  })
  const columns: ColumnDef<MailRecord, unknown>[] = [
    {
      accessorKey: 'email',
      header: t('Recipient'),
      cell: ({ row }) => (
        <div className='min-w-0'>
          <div className='break-all'>{row.original.email}</div>
          <div className='text-muted-foreground text-xs'>
            {row.original.username || row.original.user_id || '-'}
          </div>
        </div>
      ),
    },
    {
      accessorKey: 'subject',
      header: t('Subject'),
      cell: ({ row }) => (
        <Button
          variant='link'
          className='h-auto max-w-full justify-start px-0 text-start break-words whitespace-normal'
          onClick={() => setDetail(row.original.id)}
        >
          {row.original.subject}
        </Button>
      ),
    },
    {
      accessorKey: 'kind',
      header: t('Type'),
      cell: ({ row }) =>
        t(MAIL_KIND_LABELS[row.original.kind] ?? 'System notification'),
    },
    {
      accessorKey: 'source',
      header: t('Source'),
      cell: ({ row }) =>
        t(row.original.source === 'admin' ? 'Administrator' : 'System'),
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => <MailStatus status={row.original.status} />,
    },
    {
      accessorKey: 'created_at',
      header: t('Time'),
      cell: ({ row }) => formatTimestampToDate(row.original.created_at),
    },
  ]
  const { table } = useDataTable({
    data: query.data?.items ?? EMPTY,
    columns,
    totalCount: query.data?.total ?? 0,
    manualPagination: true,
    manualFiltering: true,
    pagination,
    onPaginationChange: setPagination,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <div className='space-y-4'>
      <form
        className='flex flex-wrap items-end gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          setFilters(draft)
          setPagination((value) => ({ ...value, pageIndex: 0 }))
        }}
      >
        <Input
          aria-label={t('Search email records')}
          placeholder={t('Search user, email or subject')}
          className='w-full sm:w-64'
          value={draft.keyword}
          onChange={(event) =>
            setDraft({ ...draft, keyword: event.target.value })
          }
        />
        <Input
          aria-label={t('User ID')}
          placeholder={t('User ID')}
          type='number'
          min={1}
          className='w-28'
          value={draft.user_id}
          onChange={(event) =>
            setDraft({ ...draft, user_id: event.target.value })
          }
        />
        <NativeSelect
          aria-label={t('Status')}
          value={draft.status}
          onChange={(event) =>
            setDraft({ ...draft, status: event.target.value })
          }
        >
          <NativeSelectOption value=''>{t('All Status')}</NativeSelectOption>
          {Object.entries(MAIL_STATUS_LABELS)
            .filter(([status]) => status !== 'prepared')
            .map(([status, key]) => (
              <NativeSelectOption key={status} value={status}>
                {t(key)}
              </NativeSelectOption>
            ))}
        </NativeSelect>
        <NativeSelect
          aria-label={t('Type')}
          value={draft.kind}
          onChange={(event) => setDraft({ ...draft, kind: event.target.value })}
        >
          <NativeSelectOption value=''>{t('All Types')}</NativeSelectOption>
          {Object.entries(MAIL_KIND_LABELS).map(([kind, key]) => (
            <NativeSelectOption key={kind} value={kind}>
              {t(key)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect
          aria-label={t('Source')}
          value={draft.source}
          onChange={(event) =>
            setDraft({ ...draft, source: event.target.value })
          }
        >
          <NativeSelectOption value=''>{t('All Sources')}</NativeSelectOption>
          <NativeSelectOption value='admin'>
            {t('Administrator')}
          </NativeSelectOption>
          <NativeSelectOption value='system'>{t('System')}</NativeSelectOption>
        </NativeSelect>
        <Input
          aria-label={t('Start date')}
          className='w-auto'
          type='date'
          value={draft.start}
          onChange={(event) =>
            setDraft({ ...draft, start: event.target.value })
          }
        />
        <Input
          aria-label={t('End date')}
          className='w-auto'
          type='date'
          value={draft.end}
          onChange={(event) => setDraft({ ...draft, end: event.target.value })}
        />
        <Button type='submit'>{t('Search')}</Button>
        <Button
          variant='outline'
          type='button'
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
      </form>
      {query.isError ? (
        <ErrorState onRetry={() => void query.refetch()} />
      ) : (
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={query.isPending}
          isFetching={query.isFetching}
          toolbarProps={null}
          paginationInFooter={false}
          emptyTitle={t('No email records')}
        />
      )}
      <MailRecordDetail id={detail} onClose={() => setDetail(null)} />
    </div>
  )
}
