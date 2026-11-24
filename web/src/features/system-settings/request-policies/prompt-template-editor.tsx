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
import {
  Add01Icon,
  ArrowDown01Icon,
  Delete02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useRef, useState } from 'react'
import {
  useFieldArray,
  useFormContext,
  useFormState,
  useWatch,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { FieldDescription, FieldLegend, FieldSet } from '@/components/ui/field'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

type PromptTemplateFields = {
  PromptSimilarityTemplates: { text: string }[]
  PromptSimilarityTemplateMaxBytes: number
}

const templateSizeOptions = [8192, 16384, 32768, 65536] as const

function templateSizeLabel(bytes: number) {
  return `${bytes / 1024} KiB`
}

type PromptTemplateItemProps = {
  index: number
  onRemove: () => void
}

function PromptTemplateItem(props: PromptTemplateItemProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const form = useFormContext<PromptTemplateFields>()
  const maxBytes = useWatch({
    control: form.control,
    name: 'PromptSimilarityTemplateMaxBytes',
  })
  const name = `PromptSimilarityTemplates.${props.index}.text` as const
  const text = useWatch({ control: form.control, name }) ?? ''
  const state = useFormState({ control: form.control, name })
  const { invalid } = form.getFieldState(name, state)
  const [open, setOpen] = useState(props.index === 0 || !text.trim())
  const number = formatNumber(props.index + 1, locale)
  const label = t('Template {{number}}', { number })

  return (
    <Collapsible
      open={open || invalid}
      onOpenChange={setOpen}
      className='min-w-0 rounded-lg border'
    >
      <div className='flex items-center gap-1 px-2'>
        <CollapsibleTrigger
          render={<Button type='button' variant='ghost' />}
          aria-label={label}
          className='group h-auto min-w-0 flex-1 justify-start gap-3 px-2 py-3 text-left'
        >
          <HugeiconsIcon
            icon={ArrowDown01Icon}
            aria-hidden='true'
            className='shrink-0 transition-transform group-aria-expanded:rotate-180'
          />
          <span className='min-w-0 flex-1'>
            <span className='block font-medium'>{label}</span>
            <span className='text-muted-foreground block truncate text-xs font-normal'>
              {text.trim().replaceAll(/\s+/g, ' ') || t('New template')}
            </span>
          </span>
        </CollapsibleTrigger>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          aria-label={t('Remove template {{number}}', { number })}
          onClick={props.onRemove}
        >
          <HugeiconsIcon icon={Delete02Icon} aria-hidden='true' />
        </Button>
      </div>
      <CollapsibleContent keepMounted className='px-4 pb-4'>
        <FormField
          control={form.control}
          name={name}
          render={({ field }) => (
            <FormItem>
              <FormLabel className='sr-only'>{label}</FormLabel>
              <FormControl>
                <Textarea
                  {...field}
                  rows={7}
                  className='max-h-96 min-h-36 resize-y'
                  placeholder={t('Paste a stable prompt fragment')}
                />
              </FormControl>
              <p className='text-muted-foreground text-right text-xs'>
                {t('{{length}} characters · {{bytes}} / {{maximum}}', {
                  length: formatNumber([...text.trim()].length, locale),
                  bytes: `${formatNumber(
                    new TextEncoder().encode(text.trim()).length,
                    locale
                  )} B`,
                  maximum: templateSizeLabel(maxBytes),
                })}
              </p>
              <FormMessage />
            </FormItem>
          )}
        />
      </CollapsibleContent>
    </Collapsible>
  )
}

export function PromptTemplateEditor() {
  const { t } = useTranslation()
  const { control, trigger } = useFormContext<PromptTemplateFields>()
  const { fields, append, remove } = useFieldArray({
    control,
    name: 'PromptSimilarityTemplates',
  })
  const addButton = useRef<HTMLButtonElement>(null)

  return (
    <FieldSet className='min-w-0 gap-3'>
      <FieldLegend variant='label'>
        {t('Dangerous prompt templates')}
      </FieldLegend>
      <FieldDescription>
        {t(
          'Each template must contain at least 80 characters. Choose a maximum size for each template below; the largest supported size is 64 KiB.'
        )}
      </FieldDescription>
      <FormField
        control={control}
        name='PromptSimilarityTemplateMaxBytes'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Maximum template size')}</FormLabel>
            <Select
              value={String(field.value)}
              onValueChange={(value) => {
                field.onChange(Number(value))
                void trigger('PromptSimilarityTemplates')
              }}
              items={templateSizeOptions.map((bytes) => ({
                value: String(bytes),
                label: templateSizeLabel(bytes),
              }))}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectGroup>
                  {templateSizeOptions.map((bytes) => (
                    <SelectItem key={bytes} value={String(bytes)}>
                      {templateSizeLabel(bytes)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />
      {fields.map((field, index) => (
        <PromptTemplateItem
          key={field.id}
          index={index}
          onRemove={() => {
            remove(index)
            addButton.current?.focus()
          }}
        />
      ))}
      <Button
        ref={addButton}
        type='button'
        variant='outline'
        className='self-start'
        onClick={() => append({ text: '' })}
      >
        <HugeiconsIcon
          icon={Add01Icon}
          data-icon='inline-start'
          aria-hidden='true'
        />
        {t('Add template')}
      </Button>
    </FieldSet>
  )
}
