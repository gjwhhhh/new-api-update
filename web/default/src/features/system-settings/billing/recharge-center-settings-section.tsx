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
*/
import { zodResolver } from '@hookform/resolvers/zod'
import type { TFunction } from 'i18next'
import { ExternalLink } from 'lucide-react'
import { useMemo } from 'react'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
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
import { Switch } from '@/components/ui/switch'
import { isValidRechargeCenterURL } from '@/features/recharge-center/lib/config'
import type { RechargeCenterDisplayMode } from '@/features/recharge-center/types'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

type RechargeCenterFormValues = {
  recharge_center_setting: {
    enabled: boolean
    url: string
    display_mode: RechargeCenterDisplayMode
  }
}

function createRechargeCenterSchema(t: TFunction) {
  return z
    .object({
      recharge_center_setting: z.object({
        enabled: z.boolean(),
        url: z
          .string()
          .trim()
          .max(2048)
          .refine(
            (value) => !value || isValidRechargeCenterURL(value),
            t('Enter a valid HTTPS URL without credentials.')
          ),
        display_mode: z.enum(['redirect', 'embed']),
      }),
    })
    .superRefine((value, context) => {
      if (
        value.recharge_center_setting.enabled &&
        !isValidRechargeCenterURL(value.recharge_center_setting.url)
      ) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          message: t(
            'A recharge center URL is required when the feature is enabled.'
          ),
          path: ['recharge_center_setting', 'url'],
        })
      }
    })
}

type RechargeCenterSettingsSectionProps = {
  defaultValues: {
    enabled: boolean
    url: string
    displayMode: RechargeCenterDisplayMode
  }
}

export function RechargeCenterSettingsSection(
  props: RechargeCenterSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const rechargeCenterSchema = useMemo(() => createRechargeCenterSchema(t), [t])
  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<RechargeCenterFormValues>({
      resolver: zodResolver(rechargeCenterSchema) as Resolver<
        RechargeCenterFormValues,
        unknown,
        RechargeCenterFormValues
      >,
      defaultValues: {
        recharge_center_setting: {
          enabled: props.defaultValues.enabled,
          url: props.defaultValues.url,
          display_mode: props.defaultValues.displayMode,
        },
      },
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | boolean,
          })
        }
      },
    })
  const enabled = form.watch('recharge_center_setting.enabled')
  const url = form.watch('recharge_center_setting.url')

  return (
    <SettingsSection title={t('Recharge Center')}>
      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit} autoComplete='off'>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save Recharge Center Settings'
          />
          <FormDirtyIndicator isDirty={isDirty} />

          <FormField
            control={form.control}
            name='recharge_center_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Recharge Center')}</FormLabel>
                  <FormDescription>
                    {t('Show a recharge center for signed-in users.')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {enabled ? (
            <SettingsFormGrid>
              <SettingsFormGridItem span='full'>
                <FormField
                  control={form.control}
                  name='recharge_center_setting.url'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Recharge Center URL')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder='https://pay.example.com/recharge'
                          inputMode='url'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('The HTTPS page to redirect to or embed for users.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SettingsFormGridItem>

              <FormField
                control={form.control}
                name='recharge_center_setting.display_mode'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Display Mode')}</FormLabel>
                    <Select
                      items={[
                        {
                          value: 'redirect',
                          label: t('Redirect to external page'),
                        },
                        { value: 'embed', label: t('Embed in this site') },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder={t('Display Mode')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='redirect'>
                            {t('Redirect to external page')}
                          </SelectItem>
                          <SelectItem value='embed'>
                            {t('Embed in this site')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t(
                        'Embedded mode requires the external site to allow framing.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <SettingsFormGridItem className='flex items-end'>
                <Button
                  variant='outline'
                  className='w-full sm:w-auto'
                  disabled={!isValidRechargeCenterURL(url)}
                  render={
                    <a
                      href={
                        isValidRechargeCenterURL(url) ? url.trim() : undefined
                      }
                      target='_blank'
                      rel='noopener noreferrer'
                    />
                  }
                >
                  <ExternalLink className='size-4' aria-hidden='true' />
                  {t('Preview')}
                </Button>
              </SettingsFormGridItem>
            </SettingsFormGrid>
          ) : null}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
