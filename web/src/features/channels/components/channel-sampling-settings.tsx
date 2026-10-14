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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { ChannelConfigurationStatus } from '../lib/channel-configuration'
import { parseModels, type ChannelFormValues } from '../lib/channel-form'
import { ChannelConfigurationStatusIndicator } from './drawers/channel-configuration'

type ChannelSamplingSettingsProps = {
  models: string[]
  disabled: boolean
  status: ChannelConfigurationStatus
  className: string
}

export function ChannelSamplingSettings(props: ChannelSamplingSettingsProps) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()

  return (
    <FormField
      control={form.control}
      name='exclude_from_sampling_models'
      render={({ field }) => {
        const selected = parseModels(field.value || '')
        return (
          <FormItem
            role='group'
            aria-label={t('Models excluded from status sampling')}
            className={props.className}
          >
            <div className='flex items-center gap-2'>
              <FormLabel>{t('Models excluded from status sampling')}</FormLabel>
              <ChannelConfigurationStatusIndicator status={props.status} />
            </div>
            <FormDescription>
              {t(
                'Selected models will not be counted in channel status / performance sampling. Routing and billing are unaffected. Models are included by default.'
              )}
            </FormDescription>
            <FormControl>
              <MultiSelect
                options={props.models.map((model) => ({
                  label: model,
                  value: model,
                }))}
                selected={selected}
                onChange={(next) => field.onChange(next.join(','))}
                placeholder={t('Select models to exclude from sampling')}
                disabled={
                  props.disabled ||
                  (props.models.length === 0 && selected.length === 0)
                }
                maxVisibleChips={8}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}
