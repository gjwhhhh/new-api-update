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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import { updateMailSettings } from '../api'
import type { MailSettings as Settings } from '../types'

const schema = z.object({
  records_enabled: z.boolean(),
  sending_enabled: z.boolean(),
  batch_limit: z.number().int().min(1).max(10000),
  interval_milliseconds: z.number().int().min(100).max(60000),
  retention_days: z.number().int().min(1).max(3650),
})
export function MailSettings(props: {
  settings: Settings
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<Settings>({
    resolver: zodResolver(schema),
    defaultValues: props.settings,
  })
  const mutation = useMutation({
    mutationFn: updateMailSettings,
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['mail', 'configuration'] })
      props.onClose()
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Email settings')}
    >
      <Form {...form}>
        <form
          className='space-y-5'
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          <FormField
            control={form.control}
            name='records_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3'>
                <FormLabel>{t('Record automatic email')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='sending_enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3'>
                <FormLabel>{t('Enable admin email sending')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='batch_limit'
            render={({ field, fieldState }) => (
              <FormItem>
                <FormLabel>{t('Maximum recipients per batch')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='number'
                    min={1}
                    max={10000}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
                </FormControl>
                {fieldState.error ? (
                  <p role='alert' className='text-destructive text-sm'>
                    {t('Invalid email settings')}
                  </p>
                ) : null}
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='interval_milliseconds'
            render={({ field, fieldState }) => (
              <FormItem>
                <FormLabel>{t('Email interval (milliseconds)')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='number'
                    min={100}
                    max={60000}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
                </FormControl>
                {fieldState.error ? (
                  <p role='alert' className='text-destructive text-sm'>
                    {t('Invalid email settings')}
                  </p>
                ) : null}
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='retention_days'
            render={({ field, fieldState }) => (
              <FormItem>
                <FormLabel>{t('Email retention (days)')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='number'
                    min={1}
                    max={3650}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
                </FormControl>
                {fieldState.error ? (
                  <p role='alert' className='text-destructive text-sm'>
                    {t('Invalid email settings')}
                  </p>
                ) : null}
              </FormItem>
            )}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Active and unknown deliveries are retained. Disabling admin sending pauses queued email; automatic security email is unaffected.'
            )}
          </p>
          <Button type='submit' disabled={mutation.isPending}>
            {t('Save')}
          </Button>
        </form>
      </Form>
    </Dialog>
  )
}
