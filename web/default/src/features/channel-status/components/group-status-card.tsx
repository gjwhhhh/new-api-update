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
import {
  ArrowDown,
  ArrowUp,
  ChevronRight,
  Eraser,
  Eye,
  EyeOff,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import {
  clearPerfMetricGroupSamples,
  updatePerfMetricGroupVisibility,
} from '../api'
import { CHANNEL_HEALTH_LABEL } from '../constants'
import { formatGroupRatio, getChannelHealth } from '../lib/health'
import type {
  ChannelHealth,
  GroupHours,
  GroupStatusItem,
} from '../types'
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
  hours: GroupHours
  isAdmin: boolean
  reorderMode: boolean
  canMoveUp: boolean
  canMoveDown: boolean
  onMoveUp: () => void
  onMoveDown: () => void
  onOpen: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [clearOpen, setClearOpen] = useState(false)
  const health = getChannelHealth(
    props.group.request_count,
    props.group.success_rate
  )
  const activeModels = props.group.models?.length ?? 0
  const visibleToUsers = props.group.visible_to_users !== false
  const hoursLabel = props.hours === 168 ? t('7d') : t('24h')

  const clearMutation = useMutation({
    mutationFn: () =>
      clearPerfMetricGroupSamples(props.group.group, props.hours),
    onSuccess: async () => {
      toast.success(t('Recent samples cleared'))
      setClearOpen(false)
      await queryClient.invalidateQueries({
        queryKey: ['channel-status', 'groups'],
      })
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to clear recent samples'))
    },
  })

  const visibilityMutation = useMutation({
    mutationFn: (nextVisible: boolean) =>
      updatePerfMetricGroupVisibility(props.group.group, nextVisible),
    onSuccess: async (_data, nextVisible) => {
      toast.success(
        nextVisible
          ? t('Group card is now visible to users')
          : t('Group card is now hidden from users')
      )
      await queryClient.invalidateQueries({
        queryKey: ['channel-status', 'groups'],
      })
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update group visibility'))
    },
  })

  const renderAdminFooter = () => {
    if (props.reorderMode) {
      return (
        <div
          className='border-border/60 flex items-center justify-end gap-2 border-t pt-3'
          onClick={(event) => event.stopPropagation()}
          onKeyDown={(event) => event.stopPropagation()}
        >
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onMoveUp}
            disabled={!props.canMoveUp}
            aria-label={t('Move up')}
          >
            <ArrowUp className='size-3.5' />
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onMoveDown}
            disabled={!props.canMoveDown}
            aria-label={t('Move down')}
          >
            <ArrowDown className='size-3.5' />
          </Button>
        </div>
      )
    }
    if (!props.isAdmin) {
      return null
    }
    return (
      <div
        className='border-border/60 flex flex-wrap items-center justify-between gap-2 border-t pt-3'
        onClick={(event) => event.stopPropagation()}
        onKeyDown={(event) => event.stopPropagation()}
      >
        <div className='flex items-center gap-2'>
          {visibleToUsers ? (
            <Eye className='text-muted-foreground size-3.5' />
          ) : (
            <EyeOff className='text-muted-foreground size-3.5' />
          )}
          <span className='text-muted-foreground text-xs'>
            {t('Visible to users')}
          </span>
          <Switch
            checked={visibleToUsers}
            disabled={visibilityMutation.isPending}
            onCheckedChange={(checked) => {
              visibilityMutation.mutate(checked)
            }}
            aria-label={t('Visible to users')}
          />
        </div>
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
    )
  }

  return (
    <>
      <div
        className={cn(
          'bg-card flex w-full flex-col gap-3.5 rounded-xl border p-4 text-left transition-[border-color,box-shadow] duration-200 hover:shadow-sm',
          BORDER_CLASS[health]
        )}
      >
        <button
          type='button'
          onClick={props.onOpen}
          disabled={props.reorderMode}
          className={cn(
            'flex w-full flex-col gap-3.5 text-left focus-visible:ring-ring rounded-lg focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none',
            props.reorderMode
              ? 'cursor-default'
              : 'cursor-pointer'
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

        {renderAdminFooter()}
      </div>

      <AlertDialog open={clearOpen} onOpenChange={setClearOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Clear recent samples?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will clear performance samples for group {{group}} within the current {{window}} window. Historical data outside this window is kept.',
                {
                  group: props.group.name,
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
    <div>
      <p className='text-foreground text-xs'>{props.label}</p>
      <p className='font-medium tabular-nums'>{props.value}</p>
    </div>
  )
}
