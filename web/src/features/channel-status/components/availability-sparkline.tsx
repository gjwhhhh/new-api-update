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
import { type CSSProperties, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import {
  buildAvailabilitySlots,
  type AvailabilitySlot,
} from '../lib/availability-slots'
import type { GroupBucketPoint, GroupHours } from '../types'

function getAvailabilityBarHeight(successRate: number | null) {
  if (successRate == null || !Number.isFinite(successRate)) {
    return 'h-[30%]'
  }
  if (successRate >= 99.9) {
    return 'h-full'
  }
  if (successRate >= 99) {
    return 'h-[88%]'
  }
  if (successRate >= 95) {
    return 'h-[72%]'
  }
  if (successRate >= 90) {
    return 'h-[55%]'
  }
  return 'h-[42%]'
}

function formatSlotRange(slot: AvailabilitySlot) {
  const start = new Date(slot.ts * 1000)
  const end = new Date((slot.ts + slot.spanSeconds) * 1000)
  const dateOptions: Intl.DateTimeFormatOptions = {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }
  return [
    start.toLocaleString(undefined, dateOptions),
    end.toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }),
  ].join(' – ')
}

export function AvailabilitySparkline(props: {
  series: GroupBucketPoint[]
  hours: GroupHours
  className?: string
}) {
  const { t } = useTranslation()
  const slots = useMemo(
    () => buildAvailabilitySlots(props.series),
    [props.series]
  )
  const pastLabel = props.hours === 168 ? t('7 days ago') : t('48 hours ago')

  if (slots.length === 0) {
    return (
      <div className={cn('flex flex-col gap-1', props.className)}>
        <div className='bg-muted/40 h-9 w-full rounded-md' aria-hidden='true' />
        <div className='text-muted-foreground/60 flex items-center justify-between text-[10px] font-medium tracking-wider uppercase'>
          <span>{pastLabel}</span>
          <span>{t('Now')}</span>
        </div>
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col gap-1', props.className)}>
      <TooltipProvider delay={0}>
        <div
          className='flex h-9 w-full items-end gap-[2px]'
          role='img'
          aria-label={t('Availability')}
        >
          {slots.map((slot, index) => {
            const hasData =
              slot.requestCount > 0 &&
              slot.successRate != null &&
              Number.isFinite(slot.successRate)
            return (
              <Tooltip key={slot.ts}>
                <TooltipTrigger
                  render={
                    <div className='flex h-full min-w-[2px] flex-1 items-end transition-opacity hover:opacity-75'>
                      <div
                        className={cn(
                          'channel-status-bar-grow w-full rounded-[2px]',
                          getAvailabilityBarHeight(slot.successRate),
                          hasData
                            ? getSuccessRateDotClass(
                                slot.successRate ?? Number.NaN
                              )
                            : 'bg-muted-foreground/15'
                        )}
                        style={
                          {
                            '--channel-status-bar-delay': [
                              Math.min(index * 12, 300),
                              'ms',
                            ].join(''),
                          } as CSSProperties
                        }
                        aria-hidden='true'
                      />
                    </div>
                  }
                />
                <TooltipContent side='top' className='font-mono text-xs'>
                  <p className='font-medium'>{formatSlotRange(slot)}</p>
                  <p className='mt-0.5 opacity-90'>
                    {hasData
                      ? t('{{success}}/{{total}} successful requests', {
                          success: Math.round(
                            ((slot.successRate ?? 0) / 100) * slot.requestCount
                          ),
                          total: slot.requestCount,
                        })
                      : t('No data')}
                  </p>
                </TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </TooltipProvider>
      <div className='text-muted-foreground/60 flex items-center justify-between text-[10px] font-medium tracking-wider uppercase'>
        <span>{pastLabel}</span>
        <span>{t('Now')}</span>
      </div>
    </div>
  )
}
