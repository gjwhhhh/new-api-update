import type { TFunction } from 'i18next'

import { formatNumber, formatPercent } from '@/lib/format'

import type { ChannelTestResult } from './types'

export function channelProbeUsageRows(result: ChannelTestResult, t: TFunction) {
  const input = result.probe_input_tokens
  const cached = result.probe_cached_tokens
  return [
    [
      t('Actual input tokens'),
      input == null ? t('Unknown') : formatNumber(input),
    ],
    [
      t('Cached input tokens'),
      cached == null ? t('Unknown') : formatNumber(cached),
    ],
    [
      t('Cache hit rate'),
      input != null &&
      input > 0 &&
      cached != null &&
      cached >= 0 &&
      cached <= input
        ? formatPercent((cached / input) * 100)
        : t('Unknown'),
    ],
  ]
}
