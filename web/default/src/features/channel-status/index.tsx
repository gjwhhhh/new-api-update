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
import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { getOpenAIStatus, getPerfMetricGroups } from './api'
import { LocalGroupsPanel } from './components/local-groups-panel'
import { OpenAIStatusPanel } from './components/openai-status-panel'
import { CHANNEL_STATUS_QUERY_KEYS, GROUP_HOURS_OPTIONS } from './constants'
import type { ChannelStatusTab, GroupHours } from './types'

export function ChannelStatusPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<ChannelStatusTab>('local')
  const [hours, setHours] = useState<GroupHours>(24)

  const groupsQuery = useQuery({
    queryKey: CHANNEL_STATUS_QUERY_KEYS.groups(hours),
    queryFn: () => getPerfMetricGroups(hours),
    staleTime: 60_000,
    refetchInterval: 60_000,
  })

  const openaiQuery = useQuery({
    queryKey: CHANNEL_STATUS_QUERY_KEYS.openai,
    queryFn: getOpenAIStatus,
    staleTime: 60_000,
    refetchInterval: 60_000,
  })

  const activeQuery = tab === 'local' ? groupsQuery : openaiQuery
  const updatedAt = activeQuery.dataUpdatedAt
  const isRefreshing = activeQuery.isFetching

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Channel Status')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {tab === 'local' ? (
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
        ) : null}
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => {
            void activeQuery.refetch()
          }}
          disabled={isRefreshing}
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
              groups={groupsQuery.data?.groups ?? []}
              hours={hours}
              isLoading={groupsQuery.isLoading}
              isError={groupsQuery.isError}
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
