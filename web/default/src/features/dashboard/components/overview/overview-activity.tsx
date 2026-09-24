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
import { Link } from '@tanstack/react-router'
import dayjs from 'dayjs'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'

import { Button } from '@/components/ui/button'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getUserQuotaDates } from '@/features/dashboard/api'
import { buildDailyRequestTrend } from '@/features/dashboard/lib/overview-activity'
import { getUserLogs } from '@/features/usage-logs/api'
import { LOG_TYPE_ENUM } from '@/features/usage-logs/constants'
import { usageLogSchema } from '@/features/usage-logs/data/schema'
import { formatNumber, formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { ApiInfoPanel } from './api-info-panel'

export function OverviewActivity(props: { showApiInfo: boolean }) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [days, setDays] = useState(7)
  const range = useMemo(() => {
    const end = dayjs()
    return { start: end.startOf('day').subtract(days - 1, 'day'), end }
  }, [days])
  const usageQuery = useQuery({
    queryKey: [
      'dashboard',
      'overview',
      'activity',
      userId,
      range.start.unix(),
      range.end.unix(),
    ],
    enabled: Boolean(userId),
    queryFn: async () => {
      const result = await getUserQuotaDates({
        start_timestamp: range.start.unix(),
        end_timestamp: range.end.unix(),
        default_time: 'day',
      })
      if (!result.success) throw new Error('Failed to load usage')
      return result.data
    },
    staleTime: 60_000,
  })
  const logsQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'recent-consumption', userId],
    enabled: Boolean(userId),
    queryFn: async () => {
      const result = await getUserLogs({
        p: 1,
        page_size: 5,
        type: LOG_TYPE_ENUM.CONSUME,
      })
      if (!result.success) throw new Error('Failed to load logs')
      return usageLogSchema.array().parse(result.data?.items ?? [])
    },
    staleTime: 60_000,
  })
  const chartData = useMemo(
    () =>
      buildDailyRequestTrend(usageQuery.data ?? [], range.start.unix(), days),
    [days, range.start, usageQuery.data]
  )

  return (
    <>
      <div
        className={
          props.showApiInfo
            ? 'grid min-w-0 gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]'
            : 'min-w-0'
        }
      >
        <section className='bg-card min-w-0 rounded-xl border p-4 sm:p-5'>
          <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
            <h3 className='font-semibold'>{t('Usage trend')}</h3>
            <div className='flex gap-1' aria-label={t('Time')}>
              {[7, 30].map((value) => (
                <Button
                  key={value}
                  size='sm'
                  variant={days === value ? 'secondary' : 'ghost'}
                  aria-pressed={days === value}
                  onClick={() => setDays(value)}
                >
                  {value} {t('days')}
                </Button>
              ))}
            </div>
          </div>
          {usageQuery.isPending && <Skeleton className='h-56 w-full' />}
          {usageQuery.isError && (
            <div
              className='flex h-56 flex-col items-center justify-center gap-3'
              role='status'
            >
              <p>{t('Failed to load')}</p>
              <Button
                variant='outline'
                onClick={() => void usageQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            </div>
          )}
          {usageQuery.isSuccess && (
            <>
              {!usageQuery.data?.length && (
                <p className='text-muted-foreground mb-2 text-sm'>
                  {t('No data available')}
                </p>
              )}
              <ChartContainer
                className='h-56 w-full'
                config={{
                  requests: {
                    label: t('Requests'),
                    color: 'var(--foreground)',
                  },
                }}
              >
                <AreaChart
                  accessibilityLayer
                  data={chartData}
                  margin={{ top: 8, right: 12, left: 0, bottom: 0 }}
                >
                  <CartesianGrid vertical={false} />
                  <XAxis
                    dataKey='date'
                    tick={{ fill: 'var(--muted-foreground)' }}
                    tickLine={false}
                    axisLine={false}
                    minTickGap={24}
                  />
                  <YAxis
                    width={48}
                    tick={{ fill: 'var(--muted-foreground)' }}
                    tickLine={false}
                    axisLine={false}
                    allowDecimals={false}
                  />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Area
                    dataKey='requests'
                    type='linear'
                    stroke='var(--foreground)'
                    fill='var(--foreground)'
                    fillOpacity={0.05}
                    strokeWidth={2}
                    isAnimationActive={false}
                  />
                </AreaChart>
              </ChartContainer>
            </>
          )}
        </section>
        {props.showApiInfo && <ApiInfoPanel />}
      </div>
      <section className='bg-card min-w-0 overflow-hidden rounded-xl border'>
        <div className='flex flex-wrap items-center justify-between gap-2 px-4 py-3 sm:px-5'>
          <h3 className='font-semibold'>{t('Recent consumption')}</h3>
          <Button variant='ghost' size='sm' render={<Link to='/usage-logs' />}>
            {t('Usage Logs')}
          </Button>
        </div>
        {logsQuery.isPending && (
          <div className='p-5'>
            <Skeleton className='h-32 w-full' />
          </div>
        )}
        {logsQuery.isError && (
          <div
            className='flex items-center justify-center gap-3 p-5'
            role='status'
          >
            <span>{t('Failed to load logs')}</span>
            <Button variant='outline' onClick={() => void logsQuery.refetch()}>
              {t('Retry')}
            </Button>
          </div>
        )}
        {logsQuery.isSuccess && !logsQuery.data.length && (
          <p className='text-muted-foreground px-5 py-10 text-center text-sm'>
            {t('No data available')}
          </p>
        )}
        {logsQuery.isSuccess && logsQuery.data.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>Tokens</TableHead>
                <TableHead>{t('Cost')}</TableHead>
                <TableHead>{t('Time')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logsQuery.data.map((log) => (
                <TableRow key={log.id}>
                  <TableCell
                    className='max-w-64 truncate'
                    title={log.model_name}
                  >
                    {log.model_name || '—'}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {formatNumber(log.prompt_tokens + log.completion_tokens)}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {formatQuota(log.quota)}
                  </TableCell>
                  <TableCell className='tabular-nums'>
                    {dayjs.unix(log.created_at).format('YYYY-MM-DD HH:mm:ss')}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>
    </>
  )
}
