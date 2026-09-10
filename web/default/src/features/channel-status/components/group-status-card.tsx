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

import { GroupBadge } from '@/components/group-badge'
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
import {
  formatGroupRatio,
  getChannelHealth,
  shouldShowGroupRatio,
} from '../lib/health'
import type { ChannelHealth, GroupHours, GroupStatusItem } from '../types'
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
  const hoursLabel = props.hours === 168 ? t('7d') : t('48h')

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
          'group bg-card flex w-full flex-col gap-3.5 rounded-xl border p-4 text-left transition-[border-color,box-shadow,transform] duration-200 hover:shadow-sm',
          BORDER_CLASS[health]
        )}
      >
        <button
          type='button'
          onClick={props.onOpen}
          disabled={props.reorderMode}
          className={cn(
            'flex w-full flex-col gap-3.5 text-left focus-visible:ring-ring rounded-lg focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none',
            props.reorderMode ? 'cursor-default' : 'cursor-pointer'
          )}
        >
          <div className='flex items-start justify-between gap-2'>
            <div className='flex min-w-0 flex-col gap-1'>
              <div className='flex min-w-0 items-center gap-2'>
                <GroupBadge
                  group={props.group.group}
                  label={props.group.name}
                  size='sm'
                  copyable={false}
                />
                {shouldShowGroupRatio(props.group.ratio) ? (
                  <span
                    className='text-muted-foreground/70 shrink-0 font-mono text-[11px] tabular-nums'
                    title={t('Group ratio')}
                  >
                    {formatGroupRatio(props.group.ratio)}
                  </span>
                ) : null}
              </div>
              {props.group.description &&
              props.group.description !== props.group.group &&
              props.group.description !== props.group.name ? (
                <p
                  className='text-muted-foreground truncate text-xs'
                  title={props.group.description}
                >
                  {props.group.description}
                </p>
              ) : null}
            </div>
            <Badge
              variant='outline'
              className={cn('gap-1.5 px-2 py-0.5', HEALTH_TEXT_CLASS[health])}
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

          <div className='grid grid-cols-3 gap-2'>
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

          <div className='bg-muted/40 flex items-center justify-between gap-3 rounded-lg px-3 py-2.5'>
            <div className='flex min-w-0 flex-col gap-0.5'>
              <span className='text-muted-foreground text-[10px] font-medium tracking-wider uppercase'>
                {t('Availability')}
              </span>
              <span className='text-muted-foreground truncate text-xs'>
                {props.group.request_count > 0
                  ? t('{{success}}/{{total}} successful requests', {
                      success: props.group.success_count,
                      total: props.group.request_count,
                    })
                  : t('No requests in this window')}
              </span>
            </div>
            <span
              className={cn(
                'shrink-0 font-mono text-2xl font-semibold tabular-nums',
                props.group.request_count > 0
                  ? getSuccessRateTextClass(props.group.success_rate)
                  : 'text-muted-foreground/50'
              )}
            >
              {props.group.request_count > 0
                ? formatUptimePct(props.group.success_rate)
                : '—'}
            </span>
          </div>

          <AvailabilitySparkline
            series={props.group.series ?? []}
            hours={props.hours}
          />

          <div className='text-muted-foreground flex items-center justify-between text-xs'>
            <span>
              {t('{{count}} models with traffic', { count: activeModels })}
            </span>
            <span className='text-foreground/70 group-hover:text-foreground inline-flex items-center gap-0.5 font-medium transition-colors duration-200'>
              {t('View models')}
              <ChevronRight className='size-3.5 transition-transform duration-200 group-hover:translate-x-0.5' />
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
