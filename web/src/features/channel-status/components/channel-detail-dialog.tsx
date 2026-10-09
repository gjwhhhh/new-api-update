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

import { Dialog } from '@/components/dialog'
import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateDotClass,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import { getPerfMetricChannelDetail } from '../api'
import {
  CHANNEL_CONFIGURED_STATUS_LABEL,
  CHANNEL_HEALTH_LABEL,
  CHANNEL_STATUS_QUERY_KEYS,
} from '../constants'
import { getChannelHealth } from '../lib/health'
import type {
  ChannelConfiguredStatus,
  ChannelHealth,
  ChannelStatusItem,
  GroupHours,
  GroupModelStat,
} from '../types'
import { AvailabilitySparkline } from './availability-sparkline'

const HEALTH_VARIANT: Record<
  ChannelHealth,
  'success' | 'warning' | 'danger' | 'neutral'
> = {
  running: 'success',
  fluctuating: 'warning',
  abnormal: 'danger',
  no_data: 'neutral',
}

const CONFIGURED_STATUS_VARIANT: Record<
  ChannelConfiguredStatus,
  'success' | 'neutral' | 'danger'
> = {
  1: 'success',
  2: 'neutral',
  3: 'danger',
}

export function ChannelDetailDialog(props: {
  channel: ChannelStatusItem | null
  hours: GroupHours
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const channelId = props.channel?.channel_id ?? 0
  const detailQuery = useQuery({
    queryKey: CHANNEL_STATUS_QUERY_KEYS.channelDetail(channelId, props.hours),
    queryFn: () => getPerfMetricChannelDetail(channelId, props.hours),
    enabled: props.open && channelId > 0,
    staleTime: 60_000,
  })
  const channel = detailQuery.data?.channel ?? props.channel
  const health = channel
    ? getChannelHealth(channel.request_count, channel.success_rate)
    : 'no_data'
  const models = detailQuery.data?.models ?? []

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        channel ? (
          <span className='flex flex-wrap items-center gap-2'>
            <span>
              #{channel.channel_id}{' '}
              {channel.channel_name || t('Unnamed channel')}
            </span>
            <StatusBadge
              label={t(CHANNEL_HEALTH_LABEL[health])}
              variant={HEALTH_VARIANT[health]}
              copyable={false}
            />
          </span>
        ) : (
          t('Channel details')
        )
      }
      contentClassName='sm:max-w-3xl sm:p-5 max-h-[85dvh]'
      bodyClassName='space-y-5'
    >
      {detailQuery.isLoading ? (
        <div className='space-y-4'>
          <Skeleton className='h-28 w-full rounded-lg' />
          <Skeleton className='h-36 w-full rounded-lg' />
          <Skeleton className='h-40 w-full rounded-lg' />
        </div>
      ) : null}
      {!detailQuery.isLoading && channel ? (
        <>
          <div className='flex flex-wrap items-center gap-2'>
            <StatusBadge
              label={t(CHANNEL_CONFIGURED_STATUS_LABEL[channel.channel_status])}
              variant={CONFIGURED_STATUS_VARIANT[channel.channel_status]}
              copyable={false}
            />
            <span className='text-muted-foreground text-sm'>
              {t(getChannelTypeLabel(channel.channel_type))}
            </span>
            {channel.groups.map((group) => (
              <GroupBadge
                key={group}
                group={group}
                size='sm'
                copyable={false}
              />
            ))}
          </div>

          <div className='grid gap-4 sm:grid-cols-[1fr_1fr]'>
            <div>
              <p className='text-muted-foreground text-xs'>
                {t('Availability')}
              </p>
              <p
                className={cn(
                  'text-3xl font-semibold tabular-nums',
                  channel.request_count > 0
                    ? getSuccessRateTextClass(channel.success_rate)
                    : 'text-muted-foreground'
                )}
              >
                {channel.request_count > 0
                  ? formatUptimePct(channel.success_rate)
                  : '—'}
              </p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {channel.request_count > 0
                  ? t(
                      '{{success}}/{{total}} successful requests · last {{hours}} hours',
                      {
                        success: channel.success_count,
                        total: channel.request_count,
                        hours: props.hours,
                      }
                    )
                  : t('No requests in this window')}
              </p>
            </div>
            <div className='grid grid-cols-2 gap-2'>
              <StatTile
                label={t('Latency')}
                value={formatLatency(channel.avg_latency_ms)}
              />
              <StatTile
                label={t('TTFT')}
                value={formatLatency(channel.avg_ttft_ms)}
              />
              <StatTile
                label={t('TPS')}
                value={formatThroughput(channel.avg_tps)}
              />
              <StatTile
                label={t('Requests')}
                value={channel.request_count.toLocaleString()}
              />
            </div>
          </div>

          <AvailabilitySparkline
            series={channel.series ?? []}
            hours={props.hours}
          />

          <div>
            <div className='mb-2 flex items-center justify-between'>
              <h3 className='text-sm font-semibold'>{t('Model details')}</h3>
              <span className='text-muted-foreground text-xs'>
                {t('{{count}} models with traffic', { count: models.length })}
              </span>
            </div>
            <ChannelModelDetails
              models={models}
              isError={detailQuery.isError}
            />
          </div>
        </>
      ) : null}
    </Dialog>
  )
}

function ChannelModelDetails(props: {
  models: GroupModelStat[]
  isError: boolean
}) {
  const { t } = useTranslation()
  if (props.isError) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('Failed to load channel status')}
      </p>
    )
  }
  if (props.models.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No models with traffic in this window')}
      </p>
    )
  }
  return (
    <div className='overflow-x-auto'>
      <table className='w-full min-w-[640px] text-left text-sm'>
        <thead className='text-muted-foreground text-xs'>
          <tr className='border-b'>
            <th className='py-2 pr-3 font-medium'>{t('Model')}</th>
            <th className='py-2 pr-3 font-medium'>{t('Requests')}</th>
            <th className='py-2 pr-3 font-medium'>{t('Success rate')}</th>
            <th className='py-2 pr-3 font-medium'>{t('Avg TTFT')}</th>
            <th className='py-2 pr-3 font-medium'>{t('Avg latency')}</th>
            <th className='py-2 font-medium'>{t('TPS')}</th>
          </tr>
        </thead>
        <tbody>
          {props.models.map((model) => (
            <tr key={model.model_name} className='border-border/60 border-b'>
              <td className='py-2 pr-3 font-medium'>{model.model_name}</td>
              <td className='py-2 pr-3 tabular-nums'>
                {model.request_count.toLocaleString()}
              </td>
              <td className='py-2 pr-3'>
                <span className='inline-flex items-center gap-1.5'>
                  <span
                    className={cn(
                      'size-1.5 rounded-full',
                      getSuccessRateDotClass(model.success_rate)
                    )}
                  />
                  <span
                    className={cn(
                      'tabular-nums',
                      getSuccessRateTextClass(model.success_rate)
                    )}
                  >
                    {formatUptimePct(model.success_rate)}
                  </span>
                </span>
              </td>
              <td className='py-2 pr-3 tabular-nums'>
                {formatLatency(model.avg_ttft_ms)}
              </td>
              <td className='py-2 pr-3 tabular-nums'>
                {formatLatency(model.avg_latency_ms)}
              </td>
              <td className='py-2 tabular-nums'>
                {formatThroughput(model.avg_tps)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function StatTile(props: { label: string; value: string }) {
  return (
    <div className='bg-muted/50 rounded-lg px-3 py-2'>
      <p className='text-muted-foreground text-xs'>{props.label}</p>
      <p className='text-sm font-semibold tabular-nums'>{props.value}</p>
    </div>
  )
}
