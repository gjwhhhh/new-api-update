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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUpDown, ListOrdered, RefreshCw, Save } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  getOpenAIStatus,
  getPerfMetricGroups,
  updatePerfMetricGroupDisplayOrder,
} from './api'
import { LocalGroupsPanel } from './components/local-groups-panel'
import { OpenAIStatusPanel } from './components/openai-status-panel'
import {
  CHANNEL_STATUS_QUERY_KEYS,
  GROUP_HOURS_OPTIONS,
  GROUP_SORT_OPTIONS,
} from './constants'
import type {
  ChannelStatusTab,
  GroupHours,
  GroupSortMode,
  GroupStatusItem,
} from './types'

export function ChannelStatusPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userRole = useAuthStore((s) => s.auth.user?.role)
  const isAdmin = Boolean(userRole && userRole >= ROLE.ADMIN)
  const [tab, setTab] = useState<ChannelStatusTab>('local')
  const [hours, setHours] = useState<GroupHours>(24)
  const [sortMode, setSortMode] = useState<GroupSortMode>('custom')
  const [reorderMode, setReorderMode] = useState(false)
  const [draftGroups, setDraftGroups] = useState<GroupStatusItem[] | null>(null)

  const groupsQuery = useQuery({
    queryKey: CHANNEL_STATUS_QUERY_KEYS.groups(hours, sortMode),
    queryFn: () => getPerfMetricGroups(hours, sortMode),
    staleTime: 60_000,
    refetchInterval: reorderMode ? false : 60_000,
  })

  const openaiQuery = useQuery({
    queryKey: CHANNEL_STATUS_QUERY_KEYS.openai,
    queryFn: getOpenAIStatus,
    staleTime: 60_000,
    refetchInterval: 60_000,
  })

  useEffect(() => {
    if (!reorderMode) {
      setDraftGroups(null)
      return
    }
    if (groupsQuery.data?.groups) {
      setDraftGroups(groupsQuery.data.groups)
    }
  }, [reorderMode, groupsQuery.data?.groups])

  const activeQuery = tab === 'local' ? groupsQuery : openaiQuery
  const updatedAt = activeQuery.dataUpdatedAt
  const isRefreshing = activeQuery.isFetching
  const displayGroups =
    reorderMode && draftGroups ? draftGroups : (groupsQuery.data?.groups ?? [])

  const saveOrderMutation = useMutation({
    mutationFn: (groups: string[]) => updatePerfMetricGroupDisplayOrder(groups),
    onSuccess: async () => {
      toast.success(t('Display order saved'))
      setReorderMode(false)
      setDraftGroups(null)
      setSortMode('custom')
      await queryClient.invalidateQueries({
        queryKey: ['channel-status', 'groups'],
      })
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save display order'))
    },
  })

  const handleSaveCurrentOrder = () => {
    const groups = displayGroups.map((item) => item.group)
    if (groups.length === 0) {
      toast.error(t('No groups available'))
      return
    }
    saveOrderMutation.mutate(groups)
  }

  const moveGroup = (groupName: string, direction: -1 | 1) => {
    setDraftGroups((prev) => {
      const list = prev ? [...prev] : [...(groupsQuery.data?.groups ?? [])]
      const index = list.findIndex((item) => item.group === groupName)
      if (index < 0) return prev
      const nextIndex = index + direction
      if (nextIndex < 0 || nextIndex >= list.length) return list
      const swapped = list[index]
      list[index] = list[nextIndex]
      list[nextIndex] = swapped
      return list
    })
  }

  const renderAdminOrderActions = () => {
    if (!isAdmin || sortMode !== 'custom') {
      return null
    }
    if (reorderMode) {
      return (
        <>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => {
              setReorderMode(false)
              setDraftGroups(null)
            }}
            disabled={saveOrderMutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            size='sm'
            onClick={handleSaveCurrentOrder}
            disabled={saveOrderMutation.isPending}
          >
            <Save className='size-3.5' />
            {t('Save order')}
          </Button>
        </>
      )
    }
    return (
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => setReorderMode(true)}
      >
        <ListOrdered className='size-3.5' />
        {t('Reorder cards')}
      </Button>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Channel Status')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {tab === 'local' ? (
          <>
            <div className='bg-muted inline-flex rounded-lg p-[3px]'>
              {GROUP_HOURS_OPTIONS.map((option) => (
                <button
                  key={option}
                  type='button'
                  onClick={() => setHours(option)}
                  className={cn(
                    'h-7 rounded-md px-2.5 text-sm font-medium transition-colors',
                    hours === option
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {option === 24 ? t('24h') : t('7d')}
                </button>
              ))}
            </div>
            <div className='bg-muted inline-flex rounded-lg p-[3px]'>
              {GROUP_SORT_OPTIONS.map((option) => (
                <button
                  key={option}
                  type='button'
                  disabled={reorderMode}
                  onClick={() => {
                    setSortMode(option)
                    setReorderMode(false)
                  }}
                  className={cn(
                    'h-7 rounded-md px-2.5 text-sm font-medium transition-colors',
                    sortMode === option
                      ? 'bg-background text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground',
                    reorderMode && 'opacity-60'
                  )}
                >
                  {option === 'custom'
                    ? t('Custom order')
                    : t('Sort by traffic')}
                </button>
              ))}
            </div>
            {renderAdminOrderActions()}
            {isAdmin && sortMode === 'traffic' ? (
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={handleSaveCurrentOrder}
                disabled={saveOrderMutation.isPending}
              >
                <ArrowUpDown className='size-3.5' />
                {t('Save this order')}
              </Button>
            ) : null}
          </>
        ) : null}
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => {
            void activeQuery.refetch()
          }}
          disabled={isRefreshing || reorderMode}
        >
          <RefreshCw
            className={cn('size-3.5', isRefreshing && 'animate-spin')}
          />
          {t('Refresh')}
        </Button>
        <p className='text-foreground hidden text-xs sm:block'>
          {updatedAt
            ? t('Updated at {{time}} · auto-refreshes every minute', {
                time: new Date(updatedAt).toLocaleTimeString(undefined, {
                  hour: '2-digit',
                  minute: '2-digit',
                  second: '2-digit',
                  hour12: false,
                }),
              })
            : t('Auto-refreshes every minute')}
        </p>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs
          value={tab}
          onValueChange={(value) => setTab(value as ChannelStatusTab)}
          className='gap-4'
        >
          <TabsList>
            <TabsTrigger value='local'>{t('This site status')}</TabsTrigger>
            <TabsTrigger value='openai'>{t('Official OpenAI')}</TabsTrigger>
          </TabsList>
          <TabsContent value='local'>
            <LocalGroupsPanel
              groups={displayGroups}
              hours={hours}
              isLoading={groupsQuery.isLoading}
              isError={groupsQuery.isError}
              isAdmin={isAdmin}
              reorderMode={reorderMode}
              onMoveGroup={moveGroup}
            />
          </TabsContent>
          <TabsContent value='openai'>
            <OpenAIStatusPanel
              data={openaiQuery.data}
              isLoading={openaiQuery.isLoading}
              isError={openaiQuery.isError}
            />
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
