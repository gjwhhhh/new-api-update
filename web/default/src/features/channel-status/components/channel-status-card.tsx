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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, Eraser } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import { clearPerfMetricChannelSamples } from '../api'
import {
  CHANNEL_CONFIGURED_STATUS_LABEL,
  CHANNEL_HEALTH_LABEL,
} from '../constants'
import { getChannelHealth } from '../lib/health'
import type {
  ChannelConfiguredStatus,
  ChannelHealth,
  ChannelStatusItem,
} from '../types'
import { AvailabilitySparkline } from './availability-sparkline'

const HEALTH_DOT_CLASS: Record<ChannelHealth, string> = {
  running: 'bg-emerald-500',
  fluctuating: 'bg-amber-500',
  abnormal: 'bg-red-500',
  no_data: 'bg-muted-foreground/40',
}

const HEALTH_TEXT_CLASS: Record<ChannelHealth, string> = {
  running: 'text-emerald-600 dark:text-emerald-400',
  fluctuating: 'text-amber-600 dark:text-amber-400',
  abnormal: 'text-red-600 dark:text-red-400',
  no_data: 'text-muted-foreground',
}

const CONFIGURED_STATUS_VARIANT: Record<
  ChannelConfiguredStatus,
  'success' | 'neutral' | 'danger'
> = {
  1: 'success',
  2: 'neutral',
  3: 'danger',
}

export function ChannelStatusCard(props: {
  channel: ChannelStatusItem
  hours: 48 | 168
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [clearOpen, setClearOpen] = useState(false)
  const health = getChannelHealth(
    props.channel.request_count,
    props.channel.success_rate
  )
  const configuredStatusLabel =
    CHANNEL_CONFIGURED_STATUS_LABEL[props.channel.channel_status]
  const hoursLabel = props.hours === 168 ? t('7d') : t('48h')
  const clearMutation = useMutation({
    mutationFn: () =>
      clearPerfMetricChannelSamples(props.channel.channel_id, props.hours),
    onSuccess: async () => {
      toast.success(t('Recent samples cleared'))
      setClearOpen(false)
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['channel-status', 'channels'],
        }),
        queryClient.invalidateQueries({
          queryKey: [
            'channel-status',
            'channel-detail',
            props.channel.channel_id,
          ],
        }),
      ])
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to clear recent samples'))
    },
  })

  return (
    <>
      <div className='group bg-card hover:border-ring/40 flex w-full flex-col gap-3.5 rounded-xl border p-4 text-left transition-[border-color,box-shadow,transform] duration-200 hover:shadow-sm'>
        <button
          type='button'
          onClick={props.onOpen}
          className='focus-visible:ring-ring flex w-full flex-col gap-3.5 rounded-lg text-left focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
        >
          <div className='flex items-start justify-between gap-2'>
            <div className='min-w-0 space-y-1'>
              <div className='flex min-w-0 items-center gap-2'>
                <span className='text-muted-foreground shrink-0 font-mono text-xs tabular-nums'>
                  #{props.channel.channel_id}
                </span>
                <span
                  className='truncate text-sm font-semibold'
                  title={props.channel.channel_name}
                >
                  {props.channel.channel_name || t('Unnamed channel')}
                </span>
              </div>
              <p className='text-muted-foreground truncate text-xs'>
                {t(getChannelTypeLabel(props.channel.channel_type))}
              </p>
            </div>
            <Badge
              variant='outline'
              className={cn(
                'shrink-0 gap-1.5 px-2 py-0.5',
                HEALTH_TEXT_CLASS[health]
              )}
            >
              <span
                className={cn(
                  'size-1.5 rounded-full',
                  HEALTH_DOT_CLASS[health],
                  health !== 'no_data' &&
                    'animate-pulse motion-reduce:animate-none'
                )}
                aria-hidden='true'
              />
              {t(CHANNEL_HEALTH_LABEL[health])}
            </Badge>
          </div>

          <div className='flex flex-wrap items-center gap-1.5'>
            <StatusBadge
              label={t(configuredStatusLabel)}
              variant={CONFIGURED_STATUS_VARIANT[props.channel.channel_status]}
              copyable={false}
            />
            {props.channel.groups.map((group) => (
              <GroupBadge
                key={group}
                group={group}
                size='sm'
                copyable={false}
              />
            ))}
          </div>

          <div className='grid grid-cols-3 gap-2'>
            <Metric
              label={t('Latency')}
              value={formatLatency(props.channel.avg_latency_ms)}
            />
            <Metric
              label={t('TTFT')}
              value={formatLatency(props.channel.avg_ttft_ms)}
            />
            <Metric
              label={t('TPS')}
              value={formatThroughput(props.channel.avg_tps)}
            />
          </div>

          <div className='bg-muted/40 flex items-center justify-between gap-3 rounded-lg px-3 py-2.5'>
            <div className='flex min-w-0 flex-col gap-0.5'>
              <span className='text-muted-foreground text-[10px] font-medium tracking-wider uppercase'>
                {t('Availability')}
              </span>
              <span className='text-muted-foreground truncate text-xs'>
                {props.channel.request_count > 0
                  ? t('{{success}}/{{total}} successful requests', {
                      success: props.channel.success_count,
                      total: props.channel.request_count,
                    })
                  : t('No requests in this window')}
              </span>
            </div>
            <span
              className={cn(
                'shrink-0 font-mono text-2xl font-semibold tabular-nums',
                props.channel.request_count > 0
                  ? getSuccessRateTextClass(props.channel.success_rate)
                  : 'text-muted-foreground/50'
              )}
            >
              {props.channel.request_count > 0
                ? formatUptimePct(props.channel.success_rate)
                : '—'}
            </span>
          </div>

          <AvailabilitySparkline
            series={props.channel.series ?? []}
            hours={props.hours}
          />

          <div className='text-muted-foreground flex items-center justify-between text-xs'>
            <span>{t('Channel metrics')}</span>
            <span className='text-foreground/70 group-hover:text-foreground inline-flex items-center gap-0.5 font-medium transition-colors duration-200'>
              {t('View details')}
              <ChevronRight className='size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
            </span>
          </div>
        </button>

        <div className='border-border/60 flex justify-end border-t pt-3'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setClearOpen(true)}
            disabled={clearMutation.isPending}
          >
            <Eraser className='size-3.5' />
            {t('Clear recent samples')}
          </Button>
        </div>
      </div>

      <AlertDialog open={clearOpen} onOpenChange={setClearOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Clear recent samples?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will clear performance samples for channel #{{channelId}} within the current {{window}} window. Historical data outside this window is kept.',
                {
                  channelId: props.channel.channel_id,
                  window: hoursLabel,
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={clearMutation.isPending}
              onClick={(event) => {
                event.preventDefault()
                clearMutation.mutate()
              }}
            >
              {t('Clear')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='bg-muted/40 flex min-w-0 flex-col gap-0.5 rounded-lg px-2.5 py-2'>
      <span className='text-muted-foreground truncate text-[10px] font-medium tracking-wider uppercase'>
        {props.label}
      </span>
      <span className='text-foreground truncate font-mono text-sm font-semibold tabular-nums'>
        {props.value}
      </span>
    </div>
  )
}
