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
import { ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import { CHANNEL_HEALTH_LABEL } from '../constants'
import { formatGroupRatio, getChannelHealth } from '../lib/health'
import type { ChannelHealth, GroupStatusItem } from '../types'
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

const BORDER_CLASS: Record<ChannelHealth, string> = {
  running: 'hover:border-ring/40',
  fluctuating: 'border-amber-500/40 hover:border-amber-500/60',
  abnormal: 'border-destructive/40 hover:border-destructive/60',
  no_data: 'hover:border-ring/40',
}

export function GroupStatusCard(props: {
  group: GroupStatusItem
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const health = getChannelHealth(
    props.group.request_count,
    props.group.success_rate
  )
  const activeModels = props.group.models?.length ?? 0

  return (
    <button
      type='button'
      onClick={props.onOpen}
      className={cn(
        'bg-card flex w-full cursor-pointer flex-col gap-3.5 rounded-xl border p-4 text-left transition-[border-color,box-shadow,transform] duration-200 hover:shadow-sm active:scale-[0.99] focus-visible:ring-ring focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none',
        BORDER_CLASS[health]
      )}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='flex min-w-0 items-baseline gap-2'>
            <h3 className='truncate text-sm font-semibold'>
              {props.group.name}
            </h3>
            <span className='text-foreground shrink-0 text-xs'>
              {formatGroupRatio(props.group.ratio)}
            </span>
          </div>
          {props.group.description ? (
            <p className='text-foreground mt-1 line-clamp-2 text-xs'>
              {props.group.description}
            </p>
          ) : null}
        </div>
        <StatusBadge
          label={t(CHANNEL_HEALTH_LABEL[health])}
          variant={HEALTH_VARIANT[health]}
          copyable={false}
        />
      </div>

      <div className='grid grid-cols-3 gap-2 text-sm'>
        <Metric
          label={t('Latency')}
          value={formatLatency(props.group.avg_latency_ms)}
        />
        <Metric
          label={t('TTFT')}
          value={formatLatency(props.group.avg_ttft_ms)}
        />
        <Metric
          label={t('TPS')}
          value={formatThroughput(props.group.avg_tps)}
        />
      </div>

      <div className='flex items-end justify-between gap-3'>
        <div>
          <p className='text-foreground text-xs'>{t('Availability')}</p>
          <p className='text-foreground mt-0.5 text-xs'>
            {props.group.request_count > 0
              ? t('{{success}}/{{total}} successful requests', {
                  success: props.group.success_count,
                  total: props.group.request_count,
                })
              : t('No requests in this window')}
          </p>
        </div>
        <p
          className={cn(
            'text-2xl font-semibold tabular-nums',
            props.group.request_count > 0
              ? getSuccessRateTextClass(props.group.success_rate)
              : 'text-foreground'
          )}
        >
          {props.group.request_count > 0
            ? formatUptimePct(props.group.success_rate)
            : '—'}
        </p>
      </div>

      <AvailabilitySparkline series={props.group.series ?? []} />

      <div className='text-foreground flex items-center justify-between text-sm'>
        <span>
          {t('{{count}} models with traffic', { count: activeModels })}
        </span>
        <span className='inline-flex items-center'>
          {t('View models')}
          <ChevronRight className='size-3.5' />
        </span>
      </div>
    </button>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div>
      <p className='text-foreground text-xs'>{props.label}</p>
      <p className='font-medium tabular-nums'>{props.value}</p>
    </div>
  )
}
