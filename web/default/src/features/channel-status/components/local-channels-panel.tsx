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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { CHANNEL_TYPE_OPTIONS } from '@/features/channels/constants'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'

import { CHANNEL_METRICS_SORT_OPTIONS } from '../constants'
import type {
  ChannelHealth,
  ChannelMetricsSort,
  ChannelStatusData,
  ChannelStatusItem,
  GroupHours,
} from '../types'
import { ChannelDetailDialog } from './channel-detail-dialog'
import { ChannelStatusCard } from './channel-status-card'
import { StatusFilterBar } from './status-filter-bar'

export type ConfiguredStatusFilter =
  | ''
  | 'enabled'
  | 'auto_disabled'
  | 'manually_disabled'

const CHANNEL_SORT_LABEL: Record<ChannelMetricsSort, string> = {
  traffic: 'Sort by traffic',
  success_rate: 'Sort by success rate',
  id: 'Sort by ID',
}

export function LocalChannelsPanel(props: {
  data: ChannelStatusData | undefined
  hours: GroupHours
  isLoading: boolean
  isError: boolean
  search: string
  selectedHealth: ChannelHealth | 'all'
  configuredStatus: ConfiguredStatusFilter
  groups: string[]
  selectedGroup: string
  providerTypes: number[]
  providerType: number | null
  sort: ChannelMetricsSort
  order: 'asc' | 'desc'
  onSearchChange: (value: string) => void
  onHealthChange: (value: ChannelHealth | 'all') => void
  onConfiguredStatusChange: (value: ConfiguredStatusFilter) => void
  onGroupChange: (value: string) => void
  onProviderTypeChange: (value: number | null) => void
  onSortChange: (value: ChannelMetricsSort) => void
  onOrderChange: (value: 'asc' | 'desc') => void
  onPageChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const [openChannel, setOpenChannel] = useState<ChannelStatusItem | null>(null)
  const data = props.data
  const page = data?.page ?? 1
  const pageSize = data?.page_size ?? 24
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const providerOptions = [
    ...CHANNEL_TYPE_OPTIONS.filter((provider) =>
      props.providerTypes.includes(provider.value)
    ),
    ...props.providerTypes
      .filter(
        (providerType) =>
          !CHANNEL_TYPE_OPTIONS.some(
            (provider) => provider.value === providerType
          )
      )
      .map((providerType) => ({
        value: providerType,
        label: getChannelTypeLabel(providerType),
      })),
  ]
  const hasActiveFilters = Boolean(
    props.search ||
    props.selectedHealth !== 'all' ||
    props.configuredStatus ||
    props.selectedGroup ||
    props.providerType !== null
  )

  if (props.isLoading && !data) {
    return <ChannelStatusSkeleton />
  }

  if (props.isError && !data) {
    return (
      <EmptyState
        bordered
        title={t('Failed to load channel status')}
        description={t('Try again in a moment or refresh the page.')}
      />
    )
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          value={props.search}
          onChange={(event) => props.onSearchChange(event.target.value)}
          placeholder={t('Search channels by name or ID')}
          aria-label={t('Search channels by name or ID')}
          className='w-full sm:w-64'
        />
        <select
          value={props.configuredStatus}
          onChange={(event) =>
            props.onConfiguredStatusChange(
              event.target.value as ConfiguredStatusFilter
            )
          }
          aria-label={t('Channel configuration status')}
          className='border-input bg-background h-8 rounded-lg border px-2.5 text-sm outline-none focus-visible:ring-2'
        >
          <option value=''>{t('All configured statuses')}</option>
          <option value='enabled'>{t('Enabled')}</option>
          <option value='manually_disabled'>{t('Manually disabled')}</option>
          <option value='auto_disabled'>{t('Auto disabled')}</option>
        </select>
        <select
          value={props.selectedGroup}
          onChange={(event) => props.onGroupChange(event.target.value)}
          aria-label={t('Group')}
          className='border-input bg-background h-8 rounded-lg border px-2.5 text-sm outline-none focus-visible:ring-2'
        >
          <option value=''>{t('All Groups')}</option>
          {props.groups.map((group) => (
            <option key={group} value={group}>
              {group}
            </option>
          ))}
        </select>
        <select
          value={props.providerType ?? ''}
          onChange={(event) =>
            props.onProviderTypeChange(
              event.target.value === '' ? null : Number(event.target.value)
            )
          }
          aria-label={t('Provider')}
          className='border-input bg-background h-8 rounded-lg border px-2.5 text-sm outline-none focus-visible:ring-2'
        >
          <option value=''>{t('All Vendors')}</option>
          {providerOptions.map((provider) => (
            <option key={provider.value} value={provider.value}>
              {t(provider.label)}
            </option>
          ))}
        </select>
        <select
          value={props.sort}
          onChange={(event) =>
            props.onSortChange(event.target.value as ChannelMetricsSort)
          }
          aria-label={t('Sort channels')}
          className='border-input bg-background h-8 rounded-lg border px-2.5 text-sm outline-none focus-visible:ring-2'
        >
          {CHANNEL_METRICS_SORT_OPTIONS.map((sort) => (
            <option key={sort} value={sort}>
              {t(CHANNEL_SORT_LABEL[sort])}
            </option>
          ))}
        </select>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() =>
            props.onOrderChange(props.order === 'asc' ? 'desc' : 'asc')
          }
          aria-label={
            props.order === 'asc' ? t('Sort descending') : t('Sort ascending')
          }
        >
          {props.order === 'asc' ? t('Ascending') : t('Descending')}
        </Button>
      </div>

      <StatusFilterBar
        counts={
          data?.health_counts ?? {
            running: 0,
            fluctuating: 0,
            abnormal: 0,
            no_data: 0,
          }
        }
        selected={props.selectedHealth}
        onSelect={props.onHealthChange}
        total={total}
        totalLabel='channels'
      />

      {data?.items.length ? (
        <>
          <div className='grid max-w-7xl gap-4 md:grid-cols-2 xl:grid-cols-3'>
            {data.items.map((channel) => (
              <ChannelStatusCard
                key={channel.channel_id}
                channel={channel}
                hours={props.hours}
                onOpen={() => setOpenChannel(channel)}
              />
            ))}
          </div>
          {totalPages > 1 ? (
            <div className='flex items-center justify-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page <= 1 || props.isLoading}
                onClick={() => props.onPageChange(page - 1)}
              >
                {t('Previous')}
              </Button>
              <span className='text-muted-foreground text-sm tabular-nums'>
                {t('Page {{page}} of {{total}}', { page, total: totalPages })}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page >= totalPages || props.isLoading}
                onClick={() => props.onPageChange(page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          ) : null}
        </>
      ) : (
        <EmptyState
          bordered
          title={
            hasActiveFilters
              ? t('No matching channels')
              : t('No channels available')
          }
          description={
            hasActiveFilters
              ? t('No channels match the selected filters.')
              : t('Channel metrics will appear after requests are processed.')
          }
        />
      )}

      <ChannelDetailDialog
        channel={openChannel}
        hours={props.hours}
        open={openChannel != null}
        onOpenChange={(open) => {
          if (!open) setOpenChannel(null)
        }}
      />
    </div>
  )
}

function ChannelStatusSkeleton() {
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap gap-2'>
        <Skeleton className='h-8 w-64 rounded-lg' />
        <Skeleton className='h-8 w-36 rounded-lg' />
        <Skeleton className='h-8 w-32 rounded-lg' />
        <Skeleton className='h-8 w-32 rounded-lg' />
        <Skeleton className='h-8 w-32 rounded-lg' />
      </div>
      <div className='grid max-w-7xl gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='h-72 rounded-xl' />
        ))}
      </div>
    </div>
  )
}
