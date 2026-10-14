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
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatDateTimeStr } from '@/lib/format'

import type { ChannelFormValues } from '../lib/channel-form'

type ChannelHealthCheckSettingsProps = {
  disabled: boolean
  lastAutomaticTestTime?: number
}

export function ChannelHealthCheckSettings(
  props: ChannelHealthCheckSettingsProps
) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const [selectedMode, interval] = useWatch({
    control: form.control,
    name: ['health_check_mode', 'health_check_interval_minutes'],
  })
  const mode = selectedMode || 'inherit'
  const options = [
    { value: 'inherit', label: t('Follow global settings') },
    { value: 'scheduled', label: t('Scheduled test') },
    { value: 'passive_recovery', label: t('Passive recovery only') },
    { value: 'excluded', label: t('Exclude from automatic tests') },
  ]
  let description = t(
    'Use the system-wide channel test mode and default interval.'
  )
  if (mode === 'scheduled') {
    description = t(
      'Run background health checks even when the global mode is passive recovery.'
    )
  } else if (mode === 'passive_recovery') {
    description = t(
      'Only test this channel after it has been automatically disabled.'
    )
  } else if (mode === 'excluded') {
    description = t(
      'This channel will not be tested or recovered automatically. Manual tests remain available.'
    )
  }

  return (
    <>
      <fieldset
        disabled={props.disabled}
        className='space-y-4 disabled:opacity-60'
      >
        <FormField
          control={form.control}
          name='health_check_mode'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Automatic test strategy')}</FormLabel>
              <Select
                items={options}
                value={field.value || 'inherit'}
                disabled={props.disabled}
                onValueChange={(value) => {
                  if (!value) return
                  field.onChange(value)
                  if (value === 'inherit' || value === 'excluded') {
                    form.setValue('health_check_interval_minutes', undefined, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                }}
              >
                <FormControl>
                  <SelectTrigger onBlur={field.onBlur}>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {options.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormDescription>{description}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        {(mode === 'scheduled' || mode === 'passive_recovery') && (
          <FormField
            control={form.control}
            name='health_check_interval_minutes'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Channel test interval (minutes)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={525600}
                    step={1}
                    disabled={props.disabled}
                    name={field.name}
                    ref={field.ref}
                    value={interval ?? ''}
                    onBlur={field.onBlur}
                    onChange={(event) => {
                      field.onChange(
                        event.target.value === ''
                          ? undefined
                          : Number(event.target.value)
                      )
                    }}
                  />
                </FormControl>
                <FormDescription>
                  {t('Leave empty to use the system-wide default interval.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}
      </fieldset>
      <div className='text-muted-foreground space-y-1 border-t pt-3 text-xs'>
        <p>
          <span className='text-foreground font-medium'>
            {t('Last automatic test')}:
          </span>{' '}
          {props.lastAutomaticTestTime && props.lastAutomaticTestTime > 0
            ? formatDateTimeStr(new Date(props.lastAutomaticTestTime * 1000))
            : '-'}
        </p>
        <p>
          {t(
            "Automatic health checks use this channel's Test Model configuration."
          )}
        </p>
      </div>
    </>
  )
}
