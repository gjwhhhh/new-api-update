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
import { useId } from 'react'
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
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

export function PresetNameDialog(props: {
  name: string
  rename: boolean
  busy: boolean
  onClose: () => void
  onSave: (name: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const formId = useId()
  const schema = z.object({
    name: z
      .string()
      .trim()
      .refine(
        (name) =>
          [...name].length >= 1 &&
          [...name].length <= 40 &&
          !/[\r\n\t]/.test(name),
        t('Enter a preset name of 1–40 characters.')
      ),
  })
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { name: props.name },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !props.busy) props.onClose()
      }}
      title={props.rename ? t('Rename filter preset') : t('Save filter preset')}
      description={t('Time ranges are not saved with filter presets.')}
      footer={
        <>
          <Button
            variant='outline'
            disabled={props.busy}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button form={formId} type='submit' disabled={props.busy}>
            {t('Save changes')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={formId}
          onSubmit={form.handleSubmit(async (values) => {
            await props.onSave(values.name)
          })}
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Preset name')}</FormLabel>
                <FormControl>
                  <Input {...field} disabled={props.busy} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
