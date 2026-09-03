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

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateDotClass,
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

export function GroupDetailDialog(props: {
  group: GroupStatusItem | null
  hours: number
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const group = props.group
  const health = group
    ? getChannelHealth(group.request_count, group.success_rate)
    : 'no_data'

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        group ? (
          <span className='flex flex-wrap items-center gap-2'>
            <span>
              {group.name} {formatGroupRatio(group.ratio)}
            </span>
            <StatusBadge
              label={t(CHANNEL_HEALTH_LABEL[health])}
              variant={HEALTH_VARIANT[health]}
              copyable={false}
            />
          </span>
        ) : (
          t('Group details')
        )
      }
      description={group?.description || undefined}
      contentClassName='sm:max-w-3xl max-h-[85dvh]'
      bodyClassName='space-y-5'
    >
      {group ? (
        <>
          <div className='grid gap-4 sm:grid-cols-[1fr_1fr]'>
            <div>
              <p className='text-muted-foreground text-xs'>
                {t('Availability')}
              </p>
              <p
                className={cn(
                  'text-3xl font-semibold tabular-nums',
                  group.request_count > 0
                    ? getSuccessRateTextClass(group.success_rate)
                    : 'text-muted-foreground'
                )}
              >
                {group.request_count > 0
                  ? formatUptimePct(group.success_rate)
                  : '—'}
              </p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {group.request_count > 0
                  ? t(
                      '{{success}}/{{total}} successful requests · last {{hours}} hours',
                      {
                        success: group.success_count,
                        total: group.request_count,
                        hours: props.hours,
                      }
                    )
                  : t('No requests in this window')}
              </p>
            </div>
            <div className='grid grid-cols-2 gap-2'>
              <StatTile
                label={t('Latency')}
                value={formatLatency(group.avg_latency_ms)}
              />
              <StatTile
                label={t('TTFT')}
                value={formatLatency(group.avg_ttft_ms)}
              />
              <StatTile
                label={t('TPS')}
                value={formatThroughput(group.avg_tps)}
              />
              <StatTile
                label={t('Requests')}
                value={group.request_count.toLocaleString()}
              />
            </div>
          </div>

          <AvailabilitySparkline series={group.series ?? []} />

          <div>
            <div className='mb-2 flex items-center justify-between'>
              <h3 className='text-sm font-semibold'>{t('Model details')}</h3>
              <span className='text-muted-foreground text-xs'>
                {t('{{count}} models with traffic', {
                  count: group.models?.length ?? 0,
                })}
              </span>
            </div>
            {group.models?.length ? (
              <div className='overflow-x-auto'>
                <table className='w-full min-w-[640px] text-left text-sm'>
                  <thead className='text-muted-foreground text-xs'>
                    <tr className='border-b'>
                      <th className='py-2 pr-3 font-medium'>{t('Model')}</th>
                      <th className='py-2 pr-3 font-medium'>{t('Requests')}</th>
                      <th className='py-2 pr-3 font-medium'>
                        {t('Success rate')}
                      </th>
                      <th className='py-2 pr-3 font-medium'>{t('Avg TTFT')}</th>
                      <th className='py-2 pr-3 font-medium'>
                        {t('Avg latency')}
                      </th>
                      <th className='py-2 font-medium'>{t('TPS')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {group.models.map((model) => (
                      <tr
                        key={model.model_name}
                        className='border-border/60 border-b'
                      >
                        <td className='py-2 pr-3 font-medium'>
                          {model.model_name}
                        </td>
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
            ) : (
              <p className='text-muted-foreground text-sm'>
                {t('No models with traffic in this window')}
              </p>
            )}
          </div>
        </>
      ) : null}
    </Dialog>
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
