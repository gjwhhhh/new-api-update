/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { Plus, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import {
  CHANNEL_STATUS,
  CHANNEL_STATUS_CONFIG,
} from '@/features/channels/constants'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { UserChannelPriorityOption } from '../../types'

interface Props {
  search: string
  page: number
  totalPages: number
  options: UserChannelPriorityOption[]
  selectedChannelIds: ReadonlySet<number>
  isPending: boolean
  isFetching: boolean
  isError: boolean
  onSearchChange: (value: string) => void
  onPageChange: (page: number) => void
  onRetry: () => void
  onAdd: (option: UserChannelPriorityOption) => void
}

export function UserChannelPriorityOptions(props: Props) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return (
    <section
      className='space-y-3'
      aria-labelledby='add-channel-title'
      aria-busy={props.isFetching}
    >
      <div>
        <h3 id='add-channel-title' className='text-sm font-medium'>
          {t('Add channels')}
        </h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Search by channel name or ID. You can configure multiple channels.'
          )}
        </p>
      </div>
      <Field>
        <FieldLabel htmlFor='channel-priority-search' className='sr-only'>
          {t('Search channels')}
        </FieldLabel>
        <div className='relative'>
          <Search
            aria-hidden='true'
            className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2'
          />
          <Input
            id='channel-priority-search'
            value={props.search}
            onChange={(event) => props.onSearchChange(event.target.value)}
            placeholder={t('Search channels by name or ID...')}
            className='px-9'
          />
          {props.isFetching && !props.isPending && (
            <Spinner
              aria-hidden='true'
              className='text-muted-foreground pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2'
            />
          )}
        </div>
      </Field>

      {props.isPending && (
        <LoadingState inline message={t('Loading channels...')} />
      )}
      {props.isError && (
        <ErrorState
          className='min-h-32'
          description={t('Failed to load channels')}
          onRetry={props.onRetry}
        />
      )}
      {!props.isPending && !props.isError && (
        <div className='overflow-hidden rounded-lg border'>
          <ul className='divide-y'>
            {props.options.map((option) => {
              const selected = props.selectedChannelIds.has(option.channel_id)
              const status =
                CHANNEL_STATUS_CONFIG[
                  option.channel_status as keyof typeof CHANNEL_STATUS_CONFIG
                ] ?? CHANNEL_STATUS_CONFIG[CHANNEL_STATUS.UNKNOWN]
              return (
                <li
                  key={option.channel_id}
                  className='flex items-center gap-3 p-3'
                >
                  <div className='min-w-0 flex-1'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span
                        id={`channel-option-name-${option.channel_id}`}
                        className='truncate text-sm font-medium'
                      >
                        {option.channel_name}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        #{option.channel_id}
                      </span>
                      <StatusBadge
                        label={t(status.label)}
                        variant={status.variant}
                        size='sm'
                        copyable={false}
                      />
                    </div>
                    <p className='text-muted-foreground text-xs'>
                      {t(getChannelTypeLabel(option.channel_type))} ·{' '}
                      {t('Global priority: {{priority}}', {
                        priority: formatNumber(option.global_priority, locale),
                      })}
                    </p>
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    disabled={selected}
                    aria-labelledby={`channel-add-label-${option.channel_id} channel-option-name-${option.channel_id}`}
                    onClick={() => props.onAdd(option)}
                  >
                    {!selected && (
                      <Plus aria-hidden='true' data-icon='inline-start' />
                    )}
                    <span id={`channel-add-label-${option.channel_id}`}>
                      {selected ? t('Added') : t('Add')}
                    </span>
                  </Button>
                </li>
              )
            })}
          </ul>
          {props.options.length === 0 && (
            <p className='text-muted-foreground p-6 text-center text-sm'>
              {t('No channels found')}
            </p>
          )}
          {props.totalPages > 1 && (
            <div className='flex items-center justify-between border-t p-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Page {{page}} of {{total}}', {
                  page: formatNumber(props.page, locale),
                  total: formatNumber(props.totalPages, locale),
                })}
              </span>
              <div className='flex gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={props.page <= 1 || props.isFetching}
                  onClick={() => props.onPageChange(props.page - 1)}
                >
                  {t('Previous')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={props.page >= props.totalPages || props.isFetching}
                  onClick={() => props.onPageChange(props.page + 1)}
                >
                  {t('Next')}
                </Button>
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
