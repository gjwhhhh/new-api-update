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

import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import type { GroupBucketPoint } from '../types'

export function AvailabilitySparkline(props: {
  series: GroupBucketPoint[]
  className?: string
}) {
  const { t } = useTranslation()
  let maxRequestCount = 0
  for (const point of props.series) {
    if (point.request_count > maxRequestCount) {
      maxRequestCount = point.request_count
    }
  }

  return (
    <div className={cn('space-y-1', props.className)}>
      <div className='flex h-8 w-full items-end gap-0.5'>
        {props.series.map((point) => {
          const hasData =
            point.request_count > 0 && Number.isFinite(point.success_rate)
          let heightPct = 40
          if (hasData && maxRequestCount > 0) {
            heightPct = Math.max(
              40,
              Math.round((point.request_count / maxRequestCount) * 100)
            )
          }
          return (
            <span
              key={point.ts}
              className={cn(
                'min-w-0 flex-1 rounded-[2px]',
                hasData
                  ? getSuccessRateDotClass(point.success_rate ?? Number.NaN)
                  : 'bg-muted-foreground/25'
              )}
              style={{ height: `${heightPct}%` }}
            />
          )
        })}
      </div>
      <div className='text-foreground flex justify-between text-[11px]'>
        <span>{t('Past')}</span>
        <span>{t('Now')}</span>
      </div>
    </div>
  )
}
