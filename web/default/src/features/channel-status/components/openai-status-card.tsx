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
import { formatUptimePct } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import {
  CHANNEL_HEALTH_LABEL,
  OPENAI_COMPONENT_STATUS_LABEL,
} from '../constants'
import { getOpenAIGroupHealth } from '../lib/health'
import type { ChannelHealth, OpenAIStatusGroup } from '../types'
import { UptimeHistoryBar } from './uptime-history-bar'

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

export function OpenAIStatusCard(props: {
  group: OpenAIStatusGroup
  available: boolean
  incidentCount: number
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const statuses = (props.group.components ?? []).map(
    (component) => component.status
  )
  const health = getOpenAIGroupHealth(statuses, props.available)
  const worstStatus =
    statuses.find((status) => {
      const itemHealth = getOpenAIGroupHealth([status], props.available)
      return itemHealth === health
    }) ?? 'operational'
  const statusLabel = OPENAI_COMPONENT_STATUS_LABEL[worstStatus] ?? 'Unknown'
  const componentCount = props.group.components?.length ?? 0
  const uptimePercent = props.group.uptime_percent
  const hasUptime =
    uptimePercent != null && Number.isFinite(uptimePercent)
  const hourlySeries = props.group.hourly_series ?? []
  const series = props.group.series ?? []
  let historyBar = null
  if (hourlySeries.length > 0) {
    historyBar = (
      <UptimeHistoryBar
        hourlySeries={hourlySeries}
        uptimePercent={uptimePercent}
      />
    )
  } else if (series.length > 0) {
    historyBar = (
      <UptimeHistoryBar series={series} uptimePercent={uptimePercent} />
    )
  }

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
          <h3 className='truncate text-sm font-semibold'>
            {props.group.name === 'APIs' ? t('OpenAI APIs') : props.group.name}
          </h3>
          <p className='text-foreground mt-1 text-xs'>
            {props.incidentCount > 0
              ? t('{{count}} active incidents', { count: props.incidentCount })
              : t('No active incidents')}
          </p>
        </div>
        <StatusBadge
          label={t(CHANNEL_HEALTH_LABEL[health])}
          variant={HEALTH_VARIANT[health]}
          copyable={false}
        />
      </div>

      {hasUptime ? (
        <div className='flex items-end justify-between gap-3'>
          <div>
            <p className='text-foreground text-xs'>{t('Uptime')}</p>
            <p className='text-foreground mt-0.5 text-xs'>{t(statusLabel)}</p>
          </div>
          <p className='text-2xl font-semibold tabular-nums'>
            {formatUptimePct(uptimePercent)}
          </p>
        </div>
      ) : (
        <p className='text-foreground text-sm'>{t(statusLabel)}</p>
      )}

      {historyBar}

      <div className='text-foreground flex items-center justify-between text-sm'>
        <span>{t('{{count}} components', { count: componentCount })}</span>
        <span className='inline-flex items-center'>
          {t('View details')}
          <ChevronRight className='size-3.5' />
        </span>
      </div>
    </button>
  )
}
