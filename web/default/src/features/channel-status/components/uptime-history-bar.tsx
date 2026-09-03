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
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import { OPENAI_COMPONENT_STATUS_LABEL } from '../constants'
import type {
  OpenAIUptimeDay,
  OpenAIUptimeEvent,
  OpenAIUptimeHour,
} from '../types'

const DAY_STATUS_CLASS: Record<string, string> = {
  operational: 'bg-emerald-500',
  degraded_performance: 'bg-amber-400',
  under_maintenance: 'bg-sky-400',
  partial_outage: 'bg-orange-500',
  major_outage: 'bg-red-500',
  full_outage: 'bg-red-500',
  no_data: 'bg-muted-foreground/25',
}

type UptimeBarPoint = {
  id: string
  status: string
  label: string
  events: OpenAIUptimeEvent[]
}

export function UptimeHistoryBar(props: {
  series?: OpenAIUptimeDay[]
  hourlySeries?: OpenAIUptimeHour[]
  uptimePercent?: number | null
  compact?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const hourly = props.hourlySeries ?? []
  const daily = props.series ?? []
  const points: UptimeBarPoint[] =
    hourly.length > 0
      ? hourly.map((point) => ({
          id: point.ts,
          status: point.status,
          label: formatUptimeHour(point.ts),
          events: point.events ?? [],
        }))
      : daily.map((point) => ({
          id: point.date,
          status: point.status,
          label: formatUptimeDay(point.date),
          events: point.events ?? [],
        }))

  if (points.length === 0) {
    return null
  }

  const isHourly = hourly.length > 0
  const ariaLabel =
    props.uptimePercent != null && Number.isFinite(props.uptimePercent)
      ? t('{{percent}}% uptime', {
          percent: props.uptimePercent.toFixed(2),
        })
      : t('Uptime history')

  return (
    <div className={cn('space-y-1', props.className)}>
      <TooltipProvider delay={0}>
        <div
          className={cn(
            'flex w-full items-stretch gap-0.5',
            props.compact ? 'h-5' : 'h-8'
          )}
          role='img'
          aria-label={ariaLabel}
        >
          {points.map((point) => {
            const statusLabel =
              OPENAI_COMPONENT_STATUS_LABEL[point.status] ?? 'Unknown'
            const isEmpty = point.status === 'no_data'
            return (
              <Tooltip key={point.id}>
                <TooltipTrigger
                  render={
                    <span
                      className={cn(
                        'min-w-0 flex-1 cursor-default self-end rounded-[2px]',
                        DAY_STATUS_CLASS[point.status] ??
                          DAY_STATUS_CLASS.no_data
                      )}
                      style={{ height: isEmpty ? '40%' : '100%' }}
                    />
                  }
                />
                <TooltipContent
                  side='top'
                  className='pointer-events-auto max-w-xs text-xs'
                >
                  <p className='font-medium'>{point.label}</p>
                  <p className='mt-0.5 opacity-90'>{t(statusLabel)}</p>
                  <UptimeBarTooltipEvents
                    events={point.events}
                    status={point.status}
                  />
                </TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </TooltipProvider>
      {props.compact ? null : (
        <div className='text-foreground flex justify-between text-[11px]'>
          <span>{isHourly ? t('24 hours ago') : t('90 days ago')}</span>
          <span>{isHourly ? t('Now') : t('Today')}</span>
        </div>
      )}
    </div>
  )
}

function UptimeBarTooltipEvents(props: {
  events: OpenAIUptimeEvent[]
  status: string
}) {
  const { t } = useTranslation()
  if (props.events.length > 0) {
    return (
      <ul className='mt-2 space-y-2'>
        {props.events.map((event) => (
          <li key={`${event.name}-${event.impact_status}`}>
            {event.url ? (
              <a
                href={event.url}
                target='_blank'
                rel='noopener noreferrer'
                className='font-medium underline underline-offset-2'
                onClick={(clickEvent) => {
                  clickEvent.stopPropagation()
                }}
              >
                {event.name}
              </a>
            ) : (
              <p className='font-medium'>{event.name}</p>
            )}
            <p className='opacity-90'>
              {t(
                OPENAI_COMPONENT_STATUS_LABEL[event.impact_status] ?? 'Unknown'
              )}
              {event.component_names?.length
                ? ` · ${event.component_names.join(', ')}`
                : ''}
            </p>
            {event.incident_status ? (
              <p className='capitalize opacity-90'>{event.incident_status}</p>
            ) : null}
          </li>
        ))}
      </ul>
    )
  }
  if (props.status === 'operational') {
    return (
      <p className='mt-1 opacity-90'>{t('No active incidents')}</p>
    )
  }
  return null
}

function formatUptimeDay(date: string) {
  const parsed = new Date(`${date}T00:00:00.000Z`)
  if (Number.isNaN(parsed.getTime())) {
    return date
  }
  return parsed.toLocaleDateString(undefined, {
    timeZone: 'UTC',
    month: 'short',
    day: 'numeric',
  })
}

function formatUptimeHour(ts: string) {
  const parsed = new Date(ts)
  if (Number.isNaN(parsed.getTime())) {
    return ts
  }
  return parsed.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
