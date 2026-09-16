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
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight, History, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getChannelTestHistory, getChannelTestResult } from './api'
import type {
  ChannelTestHistoryTimeRange,
  ChannelTestResult,
  ChannelTestResultStatus,
} from './types'

const PAGE_SIZE = 20
const ACTIVE_REFRESH_MS = 5000

function resolveTimeRange(search: {
  channel_id?: number
  end_at?: number
  start_at?: number
  task_id?: string
  time_range?: ChannelTestHistoryTimeRange
}): ChannelTestHistoryTimeRange {
  if (search.time_range) return search.time_range
  if (search.start_at || search.end_at) return 'custom'
  if (search.task_id) return 'all'
  if (search.channel_id) return '7d'
  return '24h'
}

function toLocalDateTimeValue(timestamp?: number): string {
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  date.setMinutes(date.getMinutes() - date.getTimezoneOffset())
  return date.toISOString().slice(0, 16)
}

function toUnixTimestamp(value: string): number | undefined {
  if (!value) return undefined
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? undefined : Math.floor(timestamp / 1000)
}

const statusClass: Record<ChannelTestResultStatus, string> = {
  succeeded:
    'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
  failed: 'bg-destructive/10 text-destructive',
  cancelled:
    'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
}

function readableCode(result: ChannelTestResult) {
  if (
    result.upstream_http_status > 0 &&
    result.result_status_code > 0 &&
    result.upstream_http_status !== result.result_status_code
  ) {
    return `${result.upstream_http_status} → ${result.result_status_code}`
  }
  return result.result_status_code || result.upstream_http_status || '-'
}

function ResultDetails(props: { resultId?: number }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['channel-test-history', 'detail', props.resultId],
    queryFn: async () => {
      const response = await getChannelTestResult(props.resultId ?? 0)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to load test record'))
      }
      return response.data
    },
    enabled: Boolean(props.resultId),
  })
  const result = query.data

  if (query.isLoading) {
    return <Skeleton className='m-4 h-56' />
  }
  if (!result) {
    return (
      <p className='text-muted-foreground p-4'>
        {t('Test record is unavailable.')}
      </p>
    )
  }

  const rows = [
    [t('Result'), t(result.status)],
    [t('Failure type'), t(result.failure_kind)],
    [t('Upstream HTTP status'), result.upstream_http_status || '-'],
    [t('Gateway result status'), result.result_status_code || '-'],
    [t('Duration'), `${result.duration_ms} ms`],
    [t('Channel'), `#${result.channel_id} ${result.channel_name}`],
    [t('Model'), result.model_name || '-'],
    [t('Endpoint'), result.endpoint_type || result.request_path],
    [t('Streaming'), result.is_stream ? t('Yes') : t('No')],
    [t('Source'), t(result.source)],
    [t('Key index'), result.key_index ?? '-'],
    [t('Automatic action'), t(result.state_action)],
    [t('Request ID'), result.request_id || '-'],
    [t('Run ID'), result.run_id || '-'],
  ]

  return (
    <div className='overflow-y-auto px-4 pb-6'>
      <dl className='divide-y rounded-md border'>
        {rows.map(([label, value]) => (
          <div
            key={String(label)}
            className='grid grid-cols-[140px_1fr] gap-3 px-3 py-2.5'
          >
            <dt className='text-muted-foreground text-xs'>{label}</dt>
            <dd className='min-w-0 text-sm break-all'>{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

export function ChannelTestHistory() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/_authenticated/channels/test-history' })
  const navigate = useNavigate({ from: '/channels/test-history' })
  const page = search.p ?? 1
  const [referenceNow] = useState(() => Math.floor(Date.now() / 1000))
  const timeRange = resolveTimeRange(search)
  let startAt: number | undefined
  if (timeRange === '24h') {
    startAt = referenceNow - 24 * 60 * 60
  } else if (timeRange === '7d') {
    startAt = referenceNow - 7 * 24 * 60 * 60
  } else if (timeRange === '30d') {
    startAt = referenceNow - 30 * 24 * 60 * 60
  } else if (timeRange === 'custom') {
    startAt = search.start_at
  }
  const endAt = timeRange === 'custom' ? search.end_at : undefined
  const query = useQuery({
    queryKey: ['channel-test-history', search, timeRange, startAt, endAt],
    queryFn: async () => {
      const response = await getChannelTestHistory({
        p: page,
        page_size: PAGE_SIZE,
        channel_id: search.channel_id,
        run_id: search.run_id,
        task_id: search.task_id,
        status: search.status,
        source: search.source,
        model_name: search.model_name,
        start_at: startAt,
        end_at: endAt,
      })
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to load channel test history')
        )
      }
      return response.data
    },
    refetchInterval: (state) => {
      const status = state.state.data?.run?.status
      return status === 'pending' || status === 'running'
        ? ACTIVE_REFRESH_MS
        : false
    },
  })
  const data = query.data
  const pageCount = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))

  const updateSearch = (updates: Record<string, unknown>, resetPage = true) => {
    void navigate({
      search: (previous) => ({
        ...previous,
        ...(resetPage ? { p: 1 } : {}),
        ...updates,
      }),
    })
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Breadcrumb>
        <Link to='/channels'>{t('Channels')}</Link>
        <span className='text-muted-foreground'>/</span>
        <span>{t('Test history')}</span>
      </SectionPageLayout.Breadcrumb>
      <SectionPageLayout.Title>
        {t('Channel test history')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <RefreshCw
            className={cn('size-4', query.isFetching && 'animate-spin')}
          />
          {t('Refresh')}
        </Button>
        <Button variant='outline' size='sm' render={<Link to='/channels' />}>
          {t('Back to channels')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          {!data?.recording_enabled && !query.isLoading && (
            <Alert>
              <AlertTitle>{t('Channel test history is disabled')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Enable it in Monitoring & Alerts to collect new test records.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {data?.run && (
            <Alert>
              <AlertTitle>{t('Batch channel test')}</AlertTitle>
              <AlertDescription>
                {t('Completed {{processed}} of {{total}} channels', {
                  processed: data.run.processed,
                  total: data.run.total,
                })}
              </AlertDescription>
            </Alert>
          )}

          <div className='text-muted-foreground flex flex-wrap items-center gap-x-5 gap-y-1 border-y py-2 text-sm'>
            {[
              [t('Tested'), data?.summary.tested ?? 0],
              [t('Succeeded'), data?.summary.succeeded ?? 0],
              [t('Failed'), data?.summary.failed ?? 0],
              [t('Cancelled'), data?.summary.cancelled ?? 0],
            ].map(([label, value]) => (
              <span key={String(label)}>
                {label}:{' '}
                <strong className='text-foreground tabular-nums'>
                  {value}
                </strong>
              </span>
            ))}
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <NativeSelect
              value={timeRange}
              onChange={(event) =>
                updateSearch({
                  time_range: event.target.value as ChannelTestHistoryTimeRange,
                  start_at: undefined,
                  end_at: undefined,
                })
              }
            >
              <NativeSelectOption value='24h'>
                {t('Last 24 hours')}
              </NativeSelectOption>
              <NativeSelectOption value='7d'>{t('Last 7 days')}</NativeSelectOption>
              <NativeSelectOption value='30d'>
                {t('Last 30 days')}
              </NativeSelectOption>
              <NativeSelectOption value='all'>
                {t('All retained records')}
              </NativeSelectOption>
              <NativeSelectOption value='custom'>
                {t('Custom range')}
              </NativeSelectOption>
            </NativeSelect>
            {timeRange === 'custom' && (
              <>
                <Input
                  className='w-48'
                  type='datetime-local'
                  aria-label={t('From')}
                  value={toLocalDateTimeValue(search.start_at)}
                  max={toLocalDateTimeValue(search.end_at) || undefined}
                  onChange={(event) =>
                    updateSearch({
                      time_range: 'custom',
                      start_at: toUnixTimestamp(event.target.value),
                    })
                  }
                />
                <Input
                  className='w-48'
                  type='datetime-local'
                  aria-label={t('To')}
                  value={toLocalDateTimeValue(search.end_at)}
                  min={toLocalDateTimeValue(search.start_at) || undefined}
                  onChange={(event) =>
                    updateSearch({
                      time_range: 'custom',
                      end_at: toUnixTimestamp(event.target.value),
                    })
                  }
                />
              </>
            )}
            <Input
              className='w-36'
              type='number'
              min={1}
              placeholder={t('Channel ID')}
              value={search.channel_id ?? ''}
              onChange={(event) => {
                const value = Number(event.target.value)
                updateSearch({ channel_id: value > 0 ? value : undefined })
              }}
            />
            <NativeSelect
              value={search.status ?? ''}
              onChange={(event) =>
                updateSearch({ status: event.target.value || undefined })
              }
            >
              <NativeSelectOption value=''>
                {t('All results')}
              </NativeSelectOption>
              <NativeSelectOption value='failed'>
                {t('Failed only')}
              </NativeSelectOption>
              <NativeSelectOption value='succeeded'>
                {t('Succeeded only')}
              </NativeSelectOption>
              <NativeSelectOption value='cancelled'>
                {t('Cancelled only')}
              </NativeSelectOption>
            </NativeSelect>
            <NativeSelect
              value={search.source ?? ''}
              onChange={(event) =>
                updateSearch({ source: event.target.value || undefined })
              }
            >
              <NativeSelectOption value=''>
                {t('All sources')}
              </NativeSelectOption>
              <NativeSelectOption value='scheduled'>
                {t('Scheduled')}
              </NativeSelectOption>
              <NativeSelectOption value='manual_batch'>
                {t('Manual batch')}
              </NativeSelectOption>
              <NativeSelectOption value='manual_single'>
                {t('Manual single')}
              </NativeSelectOption>
            </NativeSelect>
            {(search.channel_id ||
              search.status ||
              search.source ||
              search.task_id ||
              search.run_id ||
              search.model_name ||
              search.time_range ||
              search.start_at ||
              search.end_at) && (
              <Button
                variant='ghost'
                size='sm'
                onClick={() =>
                  updateSearch({
                    channel_id: undefined,
                    status: undefined,
                    source: undefined,
                    task_id: undefined,
                    run_id: undefined,
                    model_name: undefined,
                    time_range: undefined,
                    start_at: undefined,
                    end_at: undefined,
                  })
                }
              >
                {t('Clear filters')}
              </Button>
            )}
          </div>

          <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
            {query.isLoading && (
              <div className='space-y-2 p-4'>
                {['one', 'two', 'three', 'four', 'five', 'six'].map((key) => (
                  <Skeleton key={key} className='h-10 w-full' />
                ))}
              </div>
            )}
            {!query.isLoading && !data?.items.length && (
              <Empty className='h-full min-h-64'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <History />
                  </EmptyMedia>
                  <EmptyTitle>{t('No channel test records')}</EmptyTitle>
                  <EmptyDescription>
                    {data?.recording_enabled
                      ? t('No records match the current filters.')
                      : t('Enable collection to record future channel tests.')}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
            {!query.isLoading && Boolean(data?.items.length) && (
              <Table className='min-w-[1050px]'>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Completed')}</TableHead>
                    <TableHead>{t('Result')}</TableHead>
                    <TableHead>{t('Channel')}</TableHead>
                    <TableHead>{t('Source')}</TableHead>
                    <TableHead>{t('Model / endpoint')}</TableHead>
                    <TableHead>{t('Status code')}</TableHead>
                    <TableHead>{t('Duration')}</TableHead>
                    <TableHead>{t('Automatic action')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data?.items.map((result) => (
                    <TableRow
                      key={result.id}
                      className='cursor-pointer'
                      onClick={() =>
                        updateSearch({ result_id: result.id }, false)
                      }
                    >
                      <TableCell className='whitespace-nowrap'>
                        {formatTimestampToDate(result.created_at)}
                      </TableCell>
                      <TableCell>
                        <Badge
                          className={statusClass[result.status]}
                          variant='secondary'
                        >
                          {t(result.status)}
                        </Badge>
                        {result.failure_kind !== 'none' && (
                          <div className='text-muted-foreground mt-1 text-xs'>
                            {t(result.failure_kind)}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className='font-medium'>#{result.channel_id}</div>
                        <div className='text-muted-foreground max-w-40 truncate text-xs'>
                          {result.channel_name}
                        </div>
                      </TableCell>
                      <TableCell>{t(result.source)}</TableCell>
                      <TableCell>
                        <div>{result.model_name || '-'}</div>
                        <div className='text-muted-foreground text-xs'>
                          {result.endpoint_type || result.request_path}
                        </div>
                      </TableCell>
                      <TableCell className='font-mono'>
                        {readableCode(result)}
                      </TableCell>
                      <TableCell>{result.duration_ms} ms</TableCell>
                      <TableCell>{t(result.state_action)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>

          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-xs'>
              {t('{{count}} records', { count: data?.total ?? 0 })}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='icon-sm'
                disabled={page <= 1}
                onClick={() =>
                  void navigate({
                    search: (previous) => ({ ...previous, p: page - 1 }),
                  })
                }
                aria-label={t('Previous page')}
              >
                <ChevronLeft />
              </Button>
              <span className='text-sm tabular-nums'>
                {page} / {pageCount}
              </span>
              <Button
                variant='outline'
                size='icon-sm'
                disabled={page >= pageCount}
                onClick={() =>
                  void navigate({
                    search: (previous) => ({ ...previous, p: page + 1 }),
                  })
                }
                aria-label={t('Next page')}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </div>
      </SectionPageLayout.Content>

      <Sheet
        open={Boolean(search.result_id)}
        onOpenChange={(open) => {
          if (!open) updateSearch({ result_id: undefined }, false)
        }}
      >
        <SheetContent className='sm:max-w-xl'>
          <SheetHeader>
            <SheetTitle>{t('Channel test record')}</SheetTitle>
            <SheetDescription>
              {t(
                'Safe diagnostic metadata only; request and response bodies are not stored.'
              )}
            </SheetDescription>
          </SheetHeader>
          <ResultDetails resultId={search.result_id} />
        </SheetContent>
      </Sheet>
    </SectionPageLayout>
  )
}
