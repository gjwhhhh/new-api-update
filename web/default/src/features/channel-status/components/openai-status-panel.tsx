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
import { ExternalLink } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import {
  CHANNEL_HEALTH_LABEL,
  OPENAI_COMPONENT_STATUS_LABEL,
} from '../constants'
import { getOpenAIComponentHealth, getOpenAIGroupHealth } from '../lib/health'
import type {
  ChannelHealth,
  OpenAIStatusData,
  OpenAIStatusGroup,
  OpenAIStatusIncident,
  OpenAIUptimeDay,
  OpenAIUptimeHour,
} from '../types'
import { OpenAIStatusCard } from './openai-status-card'
import { UptimeHistoryBar } from './uptime-history-bar'

const HEALTH_VARIANT: Record<
  ChannelHealth,
  'success' | 'warning' | 'danger' | 'neutral'
> = {
  running: 'success',
  fluctuating: 'warning',
  abnormal: 'danger',
  no_data: 'neutral',
}

export function OpenAIStatusPanel(props: {
  data: OpenAIStatusData | undefined
  isLoading: boolean
  isError: boolean
}) {
  const { t } = useTranslation()
  const [openGroup, setOpenGroup] = useState<OpenAIStatusGroup | null>(null)
  const groups = props.data?.groups ?? []
  const incidents = props.data?.incidents ?? []
  const selectedIncidents = openGroup
    ? incidents.filter((incident) =>
        incident.affected_groups.includes(openGroup.name)
      )
    : []
  const ongoingIds = new Set(selectedIncidents.map((incident) => incident.id))
  const history = (props.data?.history ?? []).filter(
    (item) =>
      !ongoingIds.has(item.id) &&
      (openGroup == null || item.affected_groups.includes(openGroup.name))
  )
  const historyGroups = groupHistoryByDate(history)
  const openHealth = openGroup
    ? getOpenAIGroupHealth(
        (openGroup.components ?? []).map((component) => component.status),
        props.data?.available ?? false
      )
    : 'no_data'
  const componentsHaveUptimeBar = (openGroup?.components ?? []).some(
    (component) =>
      (component.hourly_series?.length ?? 0) > 0 ||
      (component.series?.length ?? 0) > 0
  )

  if (props.isLoading) {
    return (
      <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
        <Skeleton className='h-64 rounded-xl' />
      </div>
    )
  }

  if (props.isError) {
    return (
      <EmptyState
        bordered
        title={t('Failed to load OpenAI status')}
        description={t('Try again in a moment or refresh the page.')}
      />
    )
  }

  if (!props.data?.available) {
    return (
      <EmptyState
        bordered
        title={t('Unknown')}
        description={t('Unable to load OpenAI status')}
      />
    )
  }

  if (groups.length === 0) {
    return (
      <EmptyState
        bordered
        title={t('No ChatGPT or Codex status components')}
        description={t('OpenAI did not report ChatGPT or Codex status.')}
      />
    )
  }

  return (
    <div className='space-y-4'>
      <div className='text-foreground flex flex-wrap items-center justify-between gap-2 text-sm'>
        <p>{props.data.description || t('Official status')}</p>
        <Button
          variant='link'
          size='sm'
          className='h-auto px-0'
          render={
            <a
              href={props.data.source_url || 'https://status.openai.com/'}
              target='_blank'
              rel='noopener noreferrer'
            />
          }
        >
          {t('Open official status page')}
          <ExternalLink className='size-3.5' />
        </Button>
      </div>
      <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
        {groups.map((group) => (
          <OpenAIStatusCard
            key={group.name}
            group={group}
            available={props.data?.available ?? false}
            incidentCount={
              incidents.filter((incident) =>
                incident.affected_groups.includes(group.name)
              ).length
            }
            onOpen={() => setOpenGroup(group)}
          />
        ))}
      </div>
      <Dialog
        open={openGroup != null}
        onOpenChange={(open) => {
          if (!open) setOpenGroup(null)
        }}
        title={
          openGroup ? (
            <span className='flex flex-wrap items-center gap-2'>
              <span>
                {openGroup.name}
              </span>
              <StatusBadge
                label={t(CHANNEL_HEALTH_LABEL[openHealth])}
                variant={HEALTH_VARIANT[openHealth]}
                copyable={false}
              />
            </span>
          ) : (
            t('Official status')
          )
        }
        description={props.data.description || t('Official status')}
        descriptionClassName='text-foreground'
        contentClassName='sm:max-w-3xl max-h-[85dvh]'
        bodyClassName='space-y-5'
      >
        {openGroup ? (
          <>
            {(openGroup.hourly_series?.length ?? 0) > 0 ? (
              <div>
                <div className='mb-2 flex items-center justify-between gap-3'>
                  <h3 className='text-sm font-semibold'>{t('Last 24 hours')}</h3>
                  <span className='text-foreground text-xs tabular-nums'>
                    {openGroup.uptime_percent != null &&
                    Number.isFinite(openGroup.uptime_percent)
                      ? t('{{percent}}% uptime', {
                          percent: openGroup.uptime_percent.toFixed(2),
                        })
                      : null}
                  </span>
                </div>
                <UptimeHistoryBar
                  hourlySeries={openGroup.hourly_series ?? []}
                  uptimePercent={openGroup.uptime_percent}
                />
                <div className='text-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px]'>
                  <LegendDot
                    className='bg-emerald-500'
                    label={t('Operational')}
                  />
                  <LegendDot className='bg-amber-400' label={t('Degraded')} />
                  <LegendDot
                    className='bg-orange-500'
                    label={t('Partial outage')}
                  />
                  <LegendDot className='bg-red-500' label={t('Major outage')} />
                </div>
              </div>
            ) : null}

            {(openGroup.series?.length ?? 0) > 0 ? (
              <div>
                <h3 className='mb-2 text-sm font-semibold'>
                  {t('Uptime history')}
                </h3>
                <UptimeHistoryBar
                  series={openGroup.series ?? []}
                  uptimePercent={openGroup.uptime_percent}
                />
              </div>
            ) : null}

            {(openGroup.components?.length ?? 0) > 0 ? (
              <div>
                <div className='mb-2 flex items-center justify-between'>
                  <h3 className='text-sm font-semibold'>{t('Components')}</h3>
                  <span className='text-foreground text-xs'>
                    {t('{{count}} components', {
                      count: openGroup.components?.length ?? 0,
                    })}
                  </span>
                </div>
                <div className='overflow-x-auto'>
                  <table className='w-full min-w-[520px] text-left text-sm'>
                    <thead className='text-foreground text-xs'>
                      <tr className='border-b'>
                        <th className='py-2 pr-3 font-medium'>
                          {t('Component')}
                        </th>
                        <th className='py-2 pr-3 font-medium'>
                          {t('Official status')}
                        </th>
                        {componentsHaveUptimeBar ? (
                          <th className='py-2 font-medium'>
                            {t('Last 24 hours')}
                          </th>
                        ) : null}
                      </tr>
                    </thead>
                    <tbody>
                      {(openGroup.components ?? []).map((component) => {
                        const health = getOpenAIComponentHealth(
                          component.status,
                          true
                        )
                        return (
                          <tr
                            key={component.id}
                            className='border-border/60 border-b'
                          >
                            <td className='py-2 pr-3 font-medium'>
                              <div>{component.name}</div>
                              {component.uptime_percent != null &&
                              Number.isFinite(component.uptime_percent) ? (
                                <p className='text-foreground mt-0.5 text-xs tabular-nums'>
                                  {t('{{percent}}% uptime', {
                                    percent:
                                      component.uptime_percent.toFixed(2),
                                  })}
                                </p>
                              ) : null}
                            </td>
                            <td className='py-2 pr-3'>
                              <span className='inline-flex items-center gap-1.5'>
                                <span
                                  className={cn(
                                    'size-1.5 rounded-full',
                                    health === 'running' && 'bg-emerald-500',
                                    health === 'fluctuating' && 'bg-amber-500',
                                    health === 'abnormal' && 'bg-red-500',
                                    health === 'no_data' &&
                                      'bg-muted-foreground/40'
                                  )}
                                />
                                {t(
                                  OPENAI_COMPONENT_STATUS_LABEL[
                                    component.status
                                  ] ?? 'Unknown'
                                )}
                              </span>
                            </td>
                            {componentsHaveUptimeBar ? (
                              <td className='py-2'>
                                <ComponentUptimeBar
                                  hourlySeries={component.hourly_series}
                                  series={component.series}
                                  uptimePercent={component.uptime_percent}
                                />
                              </td>
                            ) : null}
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              </div>
            ) : null}

            {selectedIncidents.length > 0 ? (
              <div>
                <h3 className='mb-2 text-sm font-semibold'>
                  {t('Active incidents')}
                </h3>
                <ul className='space-y-3'>
                  {selectedIncidents.map((incident) => (
                    <IncidentListItem key={incident.id} incident={incident} />
                  ))}
                </ul>
              </div>
            ) : null}

            {historyGroups.length > 0 ? (
              <div>
                <h3 className='mb-2 text-sm font-semibold'>
                  {t('Incident history')}
                </h3>
                <div className='space-y-4'>
                  {historyGroups.map((group) => (
                    <div key={group.date}>
                      <p className='text-foreground mb-2 text-xs font-medium'>
                        {group.date}
                      </p>
                      <ul className='space-y-3'>
                        {group.items.map((incident) => (
                          <IncidentListItem
                            key={incident.id}
                            incident={incident}
                          />
                        ))}
                      </ul>
                    </div>
                  ))}
                </div>
              </div>
            ) : null}
            <div className='flex flex-wrap gap-2'>
              <Button
                variant='outline'
                size='sm'
                render={
                  <a
                    href={props.data.source_url || 'https://status.openai.com/'}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                {t('Open official status page')}
                <ExternalLink className='size-3.5' />
              </Button>
              <Button
                variant='outline'
                size='sm'
                render={
                  <a
                    href='https://status.openai.com/history'
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                {t('View full history')}
                <ExternalLink className='size-3.5' />
              </Button>
            </div>
          </>
        ) : null}
      </Dialog>
    </div>
  )
}

function LegendDot(props: { className: string; label: string }) {
  return (
    <span className='inline-flex items-center gap-1.5'>
      <span className={cn('size-2 rounded-[2px]', props.className)} />
      {props.label}
    </span>
  )
}

function ComponentUptimeBar(props: {
  hourlySeries?: OpenAIUptimeHour[]
  series?: OpenAIUptimeDay[]
  uptimePercent?: number | null
}) {
  const hourlySeries = props.hourlySeries ?? []
  if (hourlySeries.length > 0) {
    return (
      <UptimeHistoryBar
        hourlySeries={hourlySeries}
        uptimePercent={props.uptimePercent}
        compact
      />
    )
  }
  const series = props.series ?? []
  if (series.length > 0) {
    return (
      <UptimeHistoryBar
        series={series}
        uptimePercent={props.uptimePercent}
        compact
      />
    )
  }
  return null
}

function IncidentListItem(props: { incident: OpenAIStatusIncident }) {
  const { t } = useTranslation()
  const impactLabel =
    OPENAI_COMPONENT_STATUS_LABEL[props.incident.impact]
  const components = props.incident.affected_components ?? []
  const details = [
    props.incident.status,
    impactLabel ? t(impactLabel) : props.incident.impact,
    formatIncidentTime(props.incident.updated_at),
  ].filter(Boolean)

  return (
    <li className='bg-muted/50 rounded-lg px-3 py-2'>
      <p className='text-sm font-medium'>{props.incident.name}</p>
      {details.length > 0 ? (
        <p className='text-foreground mt-1 text-xs'>{details.join(' · ')}</p>
      ) : null}
      {components.length > 0 ? (
        <p className='text-foreground mt-1 text-xs'>{components.join(', ')}</p>
      ) : null}
      {props.incident.url ? (
        <a
          href={props.incident.url}
          target='_blank'
          rel='noopener noreferrer'
          className='text-primary mt-2 inline-flex items-center gap-1 text-xs hover:underline'
        >
          {t('View incident')}
          <ExternalLink className='size-3' />
        </a>
      ) : null}
    </li>
  )
}

function groupHistoryByDate(items: OpenAIStatusIncident[]) {
  const groups: { date: string; items: OpenAIStatusIncident[] }[] = []
  const indexByDate = new Map<string, number>()
  for (const item of items) {
    const date = formatIncidentDate(item.updated_at)
    const existing = indexByDate.get(date)
    if (existing == null) {
      indexByDate.set(date, groups.length)
      groups.push({ date, items: [item] })
      continue
    }
    const group = groups[existing]
    if (!group) {
      continue
    }
    group.items.push(item)
  }
  return groups
}

function formatIncidentDate(value: string) {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

function formatIncidentTime(value: string) {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return ''
  }
  return parsed.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
