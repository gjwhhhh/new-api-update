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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { Skeleton } from '@/components/ui/skeleton'

import { getChannelHealth } from '../lib/health'
import type {
  ChannelHealth,
  GroupHours,
  GroupStatusItem,
} from '../types'
import { GroupDetailDialog } from './group-detail-dialog'
import { GroupStatusCard } from './group-status-card'
import { StatusFilterBar } from './status-filter-bar'

export function LocalGroupsPanel(props: {
  groups: GroupStatusItem[]
  hours: GroupHours
  isLoading: boolean
  isError: boolean
  isAdmin: boolean
  reorderMode: boolean
  onMoveGroup: (groupName: string, direction: -1 | 1) => void
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<ChannelHealth | 'all'>('all')
  const [openGroup, setOpenGroup] = useState<GroupStatusItem | null>(null)

  const counts = useMemo(() => {
    const next: Record<ChannelHealth, number> = {
      running: 0,
      fluctuating: 0,
      abnormal: 0,
      no_data: 0,
    }
    for (const group of props.groups) {
      next[getChannelHealth(group.request_count, group.success_rate)] += 1
    }
    return next
  }, [props.groups])

  const filtered = useMemo(() => {
    // Keep absolute positions while reordering; health filter would confuse ↑↓.
    if (props.reorderMode || selected === 'all') return props.groups
    return props.groups.filter(
      (group) =>
        getChannelHealth(group.request_count, group.success_rate) === selected
    )
  }, [props.groups, props.reorderMode, selected])

  if (props.isLoading) {
    return (
      <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='h-64 rounded-xl' />
        ))}
      </div>
    )
  }

  if (props.isError) {
    return (
      <EmptyState
        bordered
        title={t('Failed to load group status')}
        description={t('Try again in a moment or refresh the page.')}
      />
    )
  }

  if (props.groups.length === 0) {
    return (
      <EmptyState
        bordered
        title={t('No groups available')}
        description={t('No usable groups are assigned to this account.')}
      />
    )
  }

  return (
    <div className='space-y-4'>
      <StatusFilterBar
        counts={counts}
        selected={props.reorderMode ? 'all' : selected}
        onSelect={setSelected}
        total={props.groups.length}
      />
      {filtered.length === 0 ? (
        <EmptyState
          bordered
          title={t('No matching groups')}
          description={t('No groups match the selected status.')}
        />
      ) : (
        <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
          {filtered.map((group, index) => (
            <GroupStatusCard
              key={group.group}
              group={group}
              hours={props.hours}
              isAdmin={props.isAdmin}
              reorderMode={props.reorderMode}
              canMoveUp={index > 0}
              canMoveDown={index < filtered.length - 1}
              onMoveUp={() => props.onMoveGroup(group.group, -1)}
              onMoveDown={() => props.onMoveGroup(group.group, 1)}
              onOpen={() => {
                if (!props.reorderMode) setOpenGroup(group)
              }}
            />
          ))}
        </div>
      )}
      <GroupDetailDialog
        group={openGroup}
        hours={props.hours}
        open={openGroup != null}
        onOpenChange={(open) => {
          if (!open) setOpenGroup(null)
        }}
      />
    </div>
  )
}
