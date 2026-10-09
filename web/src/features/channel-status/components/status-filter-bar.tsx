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

import { cn } from '@/lib/utils'

import { CHANNEL_HEALTH_FILTERS, CHANNEL_HEALTH_LABEL } from '../constants'
import type { ChannelHealth } from '../types'

const DOT_CLASS: Record<ChannelHealth, string> = {
  running: 'bg-emerald-500',
  fluctuating: 'bg-amber-500',
  abnormal: 'bg-red-500',
  no_data: 'bg-muted-foreground/40',
}

export function StatusFilterBar(props: {
  counts: Record<ChannelHealth, number>
  selected: ChannelHealth | 'all'
  onSelect: (value: ChannelHealth | 'all') => void
  total: number
  totalLabel?: 'groups' | 'channels'
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center gap-2'>
      {CHANNEL_HEALTH_FILTERS.map((health) => {
        const active = props.selected === health
        return (
          <button
            key={health}
            type='button'
            onClick={() => props.onSelect(active ? 'all' : health)}
            className={cn(
              'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm transition-colors',
              active
                ? 'border-foreground/20 bg-muted text-foreground'
                : 'border-transparent bg-muted/50 text-foreground/80 hover:text-foreground'
            )}
          >
            <span className={cn('size-1.5 rounded-full', DOT_CLASS[health])} />
            {props.counts[health]} {t(CHANNEL_HEALTH_LABEL[health])}
          </button>
        )
      })}
      <span className='text-foreground ml-auto text-sm'>
        {props.totalLabel === 'channels'
          ? t('{{count}} channels', { count: props.total })
          : t('{{count}} groups', { count: props.total })}
      </span>
    </div>
  )
}
