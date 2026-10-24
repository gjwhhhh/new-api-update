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

import {
  formatRequestHitRate,
  formatTokenHitRate,
  getWindowStats,
} from '../lib/upstream-cache'
import type { UpstreamCacheUsageStats, UpstreamCacheWindow } from '../types'

function CacheHitMetric(props: {
  window: UpstreamCacheWindow
  stats: UpstreamCacheUsageStats | undefined
}) {
  const { t } = useTranslation()
  const windowStats = getWindowStats(props.stats, props.window)
  const label =
    props.window === '1h'
      ? t('Cache hit 1h')
      : t('Cache hit 24h')
  const value = formatRequestHitRate(windowStats)
  const tokenRate = formatTokenHitRate(windowStats)
  const hit = windowStats?.hit ?? 0
  const total = windowStats?.total ?? 0
  const pcht = windowStats?.prompt_cache_hit_tokens ?? 0

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div className='bg-muted/40 flex min-w-0 flex-col gap-0.5 rounded-lg px-2.5 py-2 text-left'>
            <span className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
              {label}
            </span>
            <span className='text-foreground truncate font-mono text-sm font-semibold tabular-nums'>
              {value}
            </span>
          </div>
        }
      />
      <TooltipContent className='max-w-xs space-y-1 text-xs'>
        <p>
          {props.window === '1h'
            ? t('Sliding ~1h window; independent of the hours filter above.')
            : t('Sliding ~24h window; independent of the hours filter above.')}
        </p>
        <p>
          {t('Request hit rate')}: {value}
          {total > 0 ? ` (${hit}/${total})` : ''}
        </p>
        <p>
          {t('Token cache rate')}: {tokenRate}
        </p>
        <p>
          {t('Prompt cache hit tokens')}: {pcht}
        </p>
        <p>{t('Grouped by final settlement group / selected channel.')}</p>
      </TooltipContent>
    </Tooltip>
  )
}

export function UpstreamCacheHitMetrics(props: {
  stats: UpstreamCacheUsageStats | undefined
}) {
  const { t } = useTranslation()

  return (
    <div className='space-y-1.5'>
      <p className='text-muted-foreground text-[10px] font-medium tracking-wider uppercase'>
        {t('Upstream cache hit (sliding)')}
      </p>
      <TooltipProvider>
        <div className='grid grid-cols-2 gap-2'>
          <CacheHitMetric window='1h' stats={props.stats} />
          <CacheHitMetric window='24h' stats={props.stats} />
        </div>
      </TooltipProvider>
    </div>
  )
}
