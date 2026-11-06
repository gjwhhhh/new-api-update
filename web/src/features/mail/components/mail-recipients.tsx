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
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getMailRecipients } from '../api'
import type { MailRecipient, RecipientSelection } from '../types'

const EMPTY: MailRecipient[] = []
export function MailRecipients(props: {
  selection: RecipientSelection
  onChange: (value: RecipientSelection) => void
  limit: number
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [draft, setDraft] = useState({ keyword: '', group: '', status: 1 })
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const query = useQuery({
    queryKey: [
      'mail',
      'recipients',
      props.selection.keyword,
      props.selection.group,
      props.selection.status,
      pagination,
    ],
    queryFn: () =>
      getMailRecipients({
        keyword: props.selection.keyword,
        group: props.selection.group,
        status: props.selection.status,
        offset: pagination.pageIndex * pagination.pageSize,
        limit: pagination.pageSize,
      }),
  })
  const columns: ColumnDef<MailRecipient, unknown>[] = [
    {
      id: 'recipient_selection',
      header: t('Select'),
      cell: ({ row }) => (
        <Checkbox
          aria-label={t('Select user {{name}}', {
            name: row.original.username,
          })}
          checked={
            props.selection.all || props.selection.ids.includes(row.original.id)
          }
          disabled={props.selection.all}
          onCheckedChange={(checked) =>
            props.onChange({
              ...props.selection,
              ids: checked
                ? [...props.selection.ids, row.original.id]
                : props.selection.ids.filter((id) => id !== row.original.id),
            })
          }
        />
      ),
    },
    {
      accessorKey: 'username',
      header: t('User'),
      cell: ({ row }) => (
        <span>
          {row.original.username} · {row.original.id}
        </span>
      ),
    },
    {
      accessorKey: 'email',
      header: t('Email'),
      cell: ({ row }) => (
        <span className='break-all'>
          {row.original.email || t('No email address')}
        </span>
      ),
    },
    { accessorKey: 'group', header: t('User Group') },
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
    <section className='min-w-0 space-y-3' aria-label={t('Recipients')}>
      <h3 className='font-medium'>{t('Recipients')}</h3>
      <form
        className='flex flex-wrap gap-2'
        onSubmit={(event) => {
          event.preventDefault()
          props.onChange({ ...props.selection, ...draft, all: false })
          setPagination((value) => ({ ...value, pageIndex: 0 }))
        }}
      >
        <Input
          className='w-full sm:w-56'
          aria-label={t('Search recipients')}
          placeholder={t('Search by user or email')}
          value={draft.keyword}
          onChange={(event) =>
            setDraft({ ...draft, keyword: event.target.value })
          }
        />
        <Input
          className='w-40'
          aria-label={t('User Group')}
          placeholder={t('User Group')}
          value={draft.group}
          onChange={(event) =>
            setDraft({ ...draft, group: event.target.value })
          }
        />
        <NativeSelect
          aria-label={t('Account status')}
          value={draft.status}
          onChange={(event) =>
            setDraft({ ...draft, status: Number(event.target.value) })
          }
        >
          <NativeSelectOption value={1}>{t('Enabled')}</NativeSelectOption>
          <NativeSelectOption value={2}>{t('Disabled')}</NativeSelectOption>
        </NativeSelect>
        <Button type='submit' variant='outline'>
          {t('Search')}
        </Button>
      </form>
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={!query.data?.total || query.data.total > props.limit}
          onClick={() =>
            props.onChange({ ...props.selection, all: true, ids: [] })
          }
        >
          {t('Select all matching users')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          onClick={() =>
            props.onChange({ ...props.selection, all: false, ids: [] })
          }
        >
          {t('Clear selection')}
        </Button>
        <span aria-live='polite' className='text-muted-foreground text-sm'>
          {t('Selected users: {{total}}', {
            total: formatNumber(
              props.selection.all
                ? (query.data?.total ?? 0)
                : props.selection.ids.length,
              locale
            ),
          })}
        </span>
      </div>
      {query.data && query.data.total > props.limit ? (
        <p className='text-muted-foreground text-sm'>
          {t('Narrow the filters to select all. Batch limit: {{limit}}', {
            limit: formatNumber(props.limit, locale),
          })}
        </p>
      ) : null}
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
          emptyTitle={t('No Users Found')}
        />
      )}
    </section>
  )
}
