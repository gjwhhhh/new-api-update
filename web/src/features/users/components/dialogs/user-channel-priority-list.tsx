/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { Route, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  CHANNEL_STATUS,
  CHANNEL_STATUS_CONFIG,
} from '@/features/channels/constants'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import {
  MAX_USER_CHANNEL_PRIORITY,
  MIN_USER_CHANNEL_PRIORITY,
  type DraftUserChannelPriority,
} from '../../lib/user-channel-priority-schema'

interface Props {
  items: DraftUserChannelPriority[]
  errors: Array<string | undefined>
  onPriorityChange: (index: number, value: string) => void
  onRemove: (index: number) => void
}

export function UserChannelPriorityList(props: Props) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return (
    <section className='space-y-3' aria-labelledby='configured-title'>
      <div>
        <h3 id='configured-title' className='text-sm font-medium'>
          {t('Configured overrides')} ({props.items.length})
        </h3>
        <p className='text-muted-foreground text-sm'>
          {t('Remove an override to inherit the channel global priority.')}
        </p>
      </div>
      {props.items.length === 0 ? (
        <EmptyState
          icon={Route}
          className='min-h-36 rounded-lg border'
          title={t('No channel priority overrides')}
          description={t(
            'Add one or more channels below to customize routing order.'
          )}
        />
      ) : (
        <div className='space-y-2'>
          {props.items.map((item, index) => {
            const status =
              CHANNEL_STATUS_CONFIG[
                item.channel_status as keyof typeof CHANNEL_STATUS_CONFIG
              ] ?? CHANNEL_STATUS_CONFIG[CHANNEL_STATUS.UNKNOWN]
            const error = props.errors[index]
            const errorId = `channel-priority-error-${item.channel_id}`
            return (
              <div
                key={item.channel_id}
                className='grid gap-3 rounded-lg border p-3 md:grid-cols-[minmax(0,1fr)_11rem_auto] md:items-start'
              >
                <div className='min-w-0 space-y-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span
                      id={`channel-priority-name-${item.channel_id}`}
                      className='truncate font-medium'
                    >
                      {item.channel_name}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      #{item.channel_id}
                    </span>
                    <StatusBadge
                      label={t(status.label)}
                      variant={status.variant}
                      size='sm'
                      copyable={false}
                    />
                  </div>
                  <p className='text-muted-foreground text-xs break-words'>
                    {t(getChannelTypeLabel(item.channel_type))} ·{' '}
                    {item.channel_group} ·{' '}
                    {t('Global priority: {{priority}}', {
                      priority: formatNumber(item.global_priority, locale),
                    })}
                  </p>
                </div>
                <Field data-invalid={Boolean(error)}>
                  <FieldLabel
                    id={`channel-priority-label-${item.channel_id}`}
                    htmlFor={`channel-priority-${item.channel_id}`}
                  >
                    {t('User priority')}
                  </FieldLabel>
                  <Input
                    id={`channel-priority-${item.channel_id}`}
                    inputMode='numeric'
                    value={item.priorityInput}
                    aria-invalid={Boolean(error)}
                    aria-describedby={error ? errorId : undefined}
                    aria-labelledby={`channel-priority-label-${item.channel_id} channel-priority-name-${item.channel_id}`}
                    onChange={(event) =>
                      props.onPriorityChange(index, event.target.value)
                    }
                  />
                  {error && (
                    <FieldError id={errorId}>
                      {t(error, {
                        min: formatNumber(MIN_USER_CHANNEL_PRIORITY, locale),
                        max: formatNumber(MAX_USER_CHANNEL_PRIORITY, locale),
                      })}
                    </FieldError>
                  )}
                </Field>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  className='text-muted-foreground hover:text-destructive justify-self-end'
                  aria-label={t('Restore inherited priority for {{name}}', {
                    name: item.channel_name,
                  })}
                  onClick={() => props.onRemove(index)}
                >
                  <Trash2 aria-hidden='true' />
                </Button>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
