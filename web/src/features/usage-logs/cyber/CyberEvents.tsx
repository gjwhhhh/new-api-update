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
import { Search } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  formatTimestamp,
  formatTimestampForInput,
  parseTimestampFromInput,
} from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { fetchCyber } from './api'
import { fallbackOutcomeLabel, fallbackReasonLabel } from './fallback-labels'

const pageSize = 25

export function CyberEvents(props: { userId: number }) {
  const { t } = useTranslation()
  const [end] = useState(() => Math.floor(Date.now() / 60_000) * 60)
  const [filters, setFilters] = useState<Record<string, number | string>>({
    start: end - 86400,
    end: end + 59,
  })
  const [page, setPage] = useState(1)
  const actions = {
    permanent: t('Permanent ban recorded'),
    armed: t('Temporary ban recorded'),
    already_active: t('Ban already active'),
    blocked: t('Request blocked locally'),
    shadow: t('Shadow mode match'),
    skipped: t('No ban applied'),
    store_failed: t('Ban write not confirmed'),
    unknown: t('Not recorded'),
    ...Object.fromEntries(
      [
        'selected',
        'submitted',
        'completed',
        'failed',
        'rejected',
        'interrupted',
      ].map((outcome) => [
        `reroute_${outcome}`,
        fallbackOutcomeLabel(outcome, t),
      ])
    ),
  }
  const reasons: Record<string, string> = {
    guard_off: t('Guard disabled'),
    out_of_scope: t('Outside policy scope'),
    unsupported_request: t('Unsupported request'),
    redis_unavailable: t('Cyber ban storage unavailable'),
    request_too_large: t('Request too large'),
    missing_session: t('No usable session fingerprint'),
    store_failed: t('Ban write not confirmed'),
  }
  const query = useQuery({
    queryKey: ['cyber', props.userId, 'events', filters, page],
    queryFn: ({ signal }) =>
      fetchCyber(
        'events',
        { ...filters, p: page, page_size: pageSize },
        signal
      ),
    retry: false,
    meta: { errorToast: false },
  })
  const data = query.data
  return (
    <div className='min-w-0 space-y-4 py-3'>
      <form
        className='grid grid-cols-1 items-end gap-3 sm:grid-cols-2 xl:grid-cols-4'
        onSubmit={(event) => {
          event.preventDefault()
          const form = new FormData(event.currentTarget)
          const start = parseTimestampFromInput(String(form.get('start')))
          const end = parseTimestampFromInput(String(form.get('end'))) + 59
          const nextFilters = {
            start,
            end,
            token_id: Number(form.get('token_id')),
            channel_id: Number(form.get('channel_id')),
            source: String(form.get('source')),
            action: String(form.get('action')),
            fingerprint: String(form.get('fingerprint')),
          }
          if (
            page === 1 &&
            Object.entries(nextFilters).every(
              ([key, value]) => filters[key] === value
            )
          ) {
            void query.refetch()
          }
          setFilters(nextFilters)
          setPage(1)
        }}
      >
        <label className='grid min-w-0 gap-1.5 text-sm'>
          {t('Start Time')}
          <Input
            name='start'
            type='datetime-local'
            required
            defaultValue={formatTimestampForInput(end - 86400)}
          />
        </label>
        <label className='grid min-w-0 gap-1.5 text-sm'>
          {t('End Time')}
          <Input
            name='end'
            type='datetime-local'
            required
            defaultValue={formatTimestampForInput(end)}
          />
        </label>
        <label className='grid gap-1.5 text-sm'>
          {t('Token ID')}
          <Input name='token_id' type='number' min={1} />
        </label>
        <label className='grid gap-1.5 text-sm'>
          {t('Channel ID')}
          <Input name='channel_id' type='number' min={1} />
        </label>
        <label className='grid gap-1.5 text-sm'>
          {t('Match source')}
          <NativeSelect className='w-full' name='source'>
            <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
            <NativeSelectOption value='code'>
              {t('Error code')}
            </NativeSelectOption>
            <NativeSelectOption value='message'>
              {t('Error message')}
            </NativeSelectOption>
          </NativeSelect>
        </label>
        <label className='grid gap-1.5 text-sm'>
          {t('Outcome')}
          <NativeSelect className='w-full' name='action'>
            <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
            {Object.entries(actions).map(([id, text]) => (
              <NativeSelectOption key={id} value={id}>
                {text}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </label>
        <label className='grid gap-1.5 text-sm'>
          {t('Session fingerprint')}
          <Input name='fingerprint' pattern='[a-f0-9]{64}' maxLength={64} />
        </label>
        <Button type='submit' variant='outline' className='justify-self-start'>
          <Search aria-hidden />
          {t('Search')}
        </Button>
      </form>
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          description={getServerErrorMessage(query.error)}
          onRetry={() => void query.refetch()}
        />
      )}
      {!query.isError && data && (
        <>
          {data.items.length === 0 ? (
            <EmptyState title={t('No Cyber events')} />
          ) : (
            <StaticDataTable
              data={data.items}
              getRowKey={(row, index) => `${row.request_id}:${index}`}
              columns={[
                {
                  id: 'time',
                  header: t('Time'),
                  cell: (row) => formatTimestamp(row.created_at),
                },
                {
                  id: 'status',
                  header: t('Outcome'),
                  cell: (row) => (
                    <div className='space-y-1'>
                      <Badge
                        variant={
                          row.action === 'blocked' ? 'destructive' : 'secondary'
                        }
                      >
                        {actions[row.action as keyof typeof actions] ??
                          row.action}
                      </Badge>
                      {row.reason && (
                        <div className='text-muted-foreground text-xs'>
                          {reasons[row.reason] ??
                            fallbackReasonLabel(row.reason, t)}
                        </div>
                      )}
                    </div>
                  ),
                },
                {
                  id: 'source',
                  header: t('Match source'),
                  cell: (row) => (
                    <div className='space-y-1'>
                      <code>{row.error_code}</code>
                      <div className='text-muted-foreground text-xs'>
                        {[row.protocol, row.source].filter(Boolean).join(' / ')}
                      </div>
                    </div>
                  ),
                },
                {
                  id: 'token',
                  header: t('Token ID'),
                  cell: (row) => (row.token_id ? `#${row.token_id}` : '–'),
                },
                {
                  id: 'channel',
                  header: t('Channel ID'),
                  cell: (row) => {
                    if (!row.fallback) {
                      return row.channel_id ? `#${row.channel_id}` : '–'
                    }
                    return (
                      <div className='space-y-1 text-xs'>
                        <div>
                          {t('Source channel')}:{' '}
                          {row.fallback.source_channel_id
                            ? `#${row.fallback.source_channel_id}`
                            : t('Unknown')}
                        </div>
                        <div>
                          {t('Fallback channel')}: #
                          {row.fallback.target_channel_id}
                        </div>
                        <div>
                          {t('Channel ID')}:{' '}
                          {row.fallback.actual_channel_id
                            ? `#${row.fallback.actual_channel_id}`
                            : '–'}
                        </div>
                        <div>
                          {t('Group')}: {row.fallback.group || '–'}
                        </div>
                      </div>
                    )
                  },
                },
                {
                  id: 'session',
                  header: t('Session fingerprint'),
                  cell: (row) =>
                    row.fingerprint ? (
                      <div className='flex items-center gap-1'>
                        <code>{row.fingerprint.slice(0, 12)}</code>
                        <CopyButton value={row.fingerprint} />
                      </div>
                    ) : (
                      '–'
                    ),
                },
                {
                  id: 'audit',
                  header: t('Request ID'),
                  cell: (row) =>
                    row.request_id ? (
                      <div className='flex items-center gap-1'>
                        <a
                          className='text-primary underline-offset-4 hover:underline'
                          href={`/conversation-audit/?request_id=${encodeURIComponent(row.request_id)}`}
                        >
                          {t('View conversation audit')}
                        </a>
                        <CopyButton
                          value={row.request_id}
                          aria-label={t('Copy to clipboard')}
                        />
                      </div>
                    ) : (
                      '–'
                    ),
                },
              ]}
            />
          )}
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              disabled={page * pageSize >= data.total}
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
