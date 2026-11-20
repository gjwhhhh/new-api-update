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
import { skipToken, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useDebounce } from '@/hooks/use-debounce'

import {
  SettingsForm,
  SettingsControlGroup,
  SettingsControlChildren,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import {
  getFilterTargets,
  type FilterTarget as FilterTargetRow,
  type FilterTargetKind,
} from './api'
import type { FilteringSettings } from './defaults'
import { promptTextFilterScopeLabel } from './policy-label'
import { useSavePolicy } from './use-save-policy'

const maxTargets = 100
const targetIDs = z.array(z.int().positive()).max(maxTargets)
const filterScopes = ['all', 'channel', 'token', 'channel_token'] as const
const filterScope = z.enum(filterScopes)
const savedIDs = targetIDs.nullish().transform((ids) => ids ?? [])
const targetSchema = z
  .object({
    scope: filterScope,
    channel_ids: savedIDs,
    token_ids: savedIDs,
  })
  .refine((target) =>
    (['channel', 'token'] as const).every(
      (kind) =>
        (target.scope === kind || target.scope === 'channel_token') ===
        target[`${kind}_ids`].length > 0
    )
  )

function parseFilterTarget(raw: string) {
  try {
    return targetSchema.parse(JSON.parse(raw))
  } catch {
    return { scope: null, channel_ids: [], token_ids: [] }
  }
}

const exemptSchema = z.object({
  channel_ids: targetIDs,
  token_ids: targetIDs,
})
const savedExemptSchema = exemptSchema.extend({
  channel_ids: savedIDs,
  token_ids: savedIDs,
})

function parseFilterExempt(raw: string) {
  try {
    return savedExemptSchema.parse(JSON.parse(raw || '{}'))
  } catch {
    return { channel_ids: [] as number[], token_ids: [] as number[] }
  }
}

function serializeFilterExempt(exempt: {
  channel_ids: number[]
  token_ids: number[]
}) {
  const channel_ids = [...new Set(exempt.channel_ids)].toSorted((a, b) => a - b)
  const token_ids = [...new Set(exempt.token_ids)].toSorted((a, b) => a - b)
  if (!channel_ids.length && !token_ids.length) {
    return '{}'
  }
  return JSON.stringify({
    channel_ids: channel_ids.length ? channel_ids : undefined,
    token_ids: token_ids.length ? token_ids : undefined,
  })
}

const sensitiveSchema = z.object({
  CheckSensitiveEnabled: z.boolean(),
  CheckSensitiveOnPromptEnabled: z.boolean(),
  SensitiveWords: z.string().optional(),
  target: z
    .object({
      scope: filterScope.nullable(),
      channel_ids: targetIDs,
      token_ids: targetIDs,
    })
    .superRefine((target, ctx) => {
      if (target.scope === null) {
        ctx.addIssue({ code: 'custom', path: ['scope'], message: 'Required' })
        return
      }
      if (target.scope === 'all') return
      for (const kind of ['channel', 'token'] as const) {
        const field = `${kind}_ids` as const
        if (
          (target.scope === kind || target.scope === 'channel_token') &&
          !target[field].length
        ) {
          ctx.addIssue({
            code: 'custom',
            path: [field],
            message: 'Select at least one target',
          })
        }
      }
    }),
  exempt: exemptSchema,
})

type SensitiveFormValues = z.infer<typeof sensitiveSchema>

function serializeFilterTarget(target: SensitiveFormValues['target']) {
  if (target.scope === null) return undefined
  return JSON.stringify({
    scope: target.scope,
    channel_ids:
      target.scope === 'channel' || target.scope === 'channel_token'
        ? [...new Set(target.channel_ids)].toSorted((a, b) => a - b)
        : undefined,
    token_ids:
      target.scope === 'token' || target.scope === 'channel_token'
        ? [...new Set(target.token_ids)].toSorted((a, b) => a - b)
        : undefined,
  })
}

type RequestChecksSectionProps = {
  defaultValues: FilteringSettings
}

type FilterTargetSelectProps = Pick<
  React.ComponentProps<typeof MultiSelect>,
  'id' | 'aria-invalid' | 'aria-describedby' | 'data-form-root'
> & {
  kind: FilterTargetKind
  ids: number[]
  onChange: (ids: number[]) => void
}

function FilterTargetSelect({
  kind,
  ids,
  onChange,
  ...inputProps
}: FilterTargetSelectProps) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const query = search.trim()
  const debounced = useDebounce(query, 250)
  const metadataKey = ['prompt-filter-targets', kind, 'metadata'] as const
  const metadata = useQuery<ReadonlyMap<number, FilterTargetRow | null>>({
    queryKey: metadataKey,
    queryFn: skipToken,
  }).data
  const unknownIDs = ids.filter((id) => !metadata?.has(id))
  const searchResults = useQuery({
    queryKey: ['prompt-filter-targets', kind, debounced],
    queryFn: ({ signal }) => getFilterTargets(kind, debounced, signal),
    enabled: open && query === debounced,
  })
  const selectedResults = useQuery({
    queryKey: ['prompt-filter-targets', kind, 'lookup', unknownIDs],
    queryFn: async ({ signal }) => {
      const rows = await getFilterTargets(kind, unknownIDs, signal)
      const byID = new Map(rows.map((row) => [row.id, row]))
      client.setQueryData<ReadonlyMap<number, FilterTargetRow | null>>(
        metadataKey,
        (previous) => {
          const next = new Map(previous)
          for (const id of unknownIDs) {
            if (!next.has(id)) next.set(id, byID.get(id) ?? null)
          }
          return next
        }
      )
      return rows
    },
    enabled: unknownIDs.length > 0,
  })
  const results = query === debounced ? searchResults.data : undefined
  const rows = new Map(metadata)
  for (const row of results ?? []) rows.set(row.id, row)
  const options = [
    ...new Set([...(results ?? []).map((row) => row.id), ...ids]),
  ].map((id) => {
    const row = rows.get(id)
    if (row) return { value: String(id), label: `${row.name} (#${id})` }
    return {
      value: String(id),
      label: row === null ? t('Unavailable target #{{id}}', { id }) : `#${id}`,
    }
  })

  return (
    <>
      <MultiSelect
        {...inputProps}
        options={options}
        selected={ids.map(String)}
        onChange={(values) => {
          const nextIDs = values.slice(0, maxTargets).map(Number)
          const picked = (results ?? []).filter((row) =>
            nextIDs.includes(row.id)
          )
          if (picked.length > 0) {
            client.setQueryData<ReadonlyMap<number, FilterTargetRow | null>>(
              metadataKey,
              (previous) => {
                const next = new Map(previous)
                for (const row of picked) next.set(row.id, row)
                return next
              }
            )
          }
          onChange(nextIDs)
        }}
        placeholder={t('Search by name or ID')}
        maxVisibleChips={4}
        remoteSearch={{ onSearchChange: setSearch, onOpenChange: setOpen }}
        emptyText={
          query !== debounced || searchResults.isFetching
            ? t('Loading...')
            : undefined
        }
      />
      {(searchResults.isError || selectedResults.isError) && (
        <p role='status' className='text-destructive text-sm'>
          {t('Failed to load filter targets')}
        </p>
      )}
    </>
  )
}

export function RequestChecksSection({
  defaultValues,
}: RequestChecksSectionProps) {
  const { t } = useTranslation()
  const updateOption = useSavePolicy()
  const {
    CheckSensitiveEnabled,
    CheckSensitiveOnPromptEnabled,
    SensitiveWords,
    PromptTextFilterTarget,
    PromptTextFilterExempt,
  } = defaultValues
  const initialValues = useMemo(
    () => ({
      CheckSensitiveEnabled,
      CheckSensitiveOnPromptEnabled,
      SensitiveWords,
      target: parseFilterTarget(PromptTextFilterTarget),
      exempt: parseFilterExempt(PromptTextFilterExempt),
    }),
    [
      CheckSensitiveEnabled,
      CheckSensitiveOnPromptEnabled,
      SensitiveWords,
      PromptTextFilterTarget,
      PromptTextFilterExempt,
    ]
  )
  const form = useForm<SensitiveFormValues>({
    resolver: zodResolver(sensitiveSchema),
    defaultValues: initialValues,
  })
  const scope = useWatch({ control: form.control, name: 'target.scope' })
  const scopes = filterScopes.map((value) => ({
    value,
    label: promptTextFilterScopeLabel(t, value),
  }))

  useEffect(() => {
    form.reset(initialValues)
  }, [initialValues, form])

  const onSubmit = async ({ target, exempt, ...values }: SensitiveFormValues) => {
    const options = Object.fromEntries(
      Object.entries(values)
        .filter(
          ([key, value]) =>
            value !== defaultValues[key as keyof FilteringSettings]
        )
        .map(([key, value]) => [key, String(value ?? '')])
    )
    const serializedTarget = serializeFilterTarget(target)
    if (
      serializedTarget &&
      serializedTarget !== serializeFilterTarget(initialValues.target)
    ) {
      options.PromptTextFilterTarget = serializedTarget
    }
    const serializedExempt = serializeFilterExempt(exempt)
    if (serializedExempt !== serializeFilterExempt(initialValues.exempt)) {
      options.PromptTextFilterExempt = serializedExempt
    }
    if (Object.keys(options).length) {
      // useSavePolicy handles server errors and refreshes the settings cache.
      await updateOption.mutateAsync(options).catch(() => undefined)
    }
  }

  return (
    <SettingsSection title={t('Request checks')}>
      <p className='text-muted-foreground text-sm'>
        {t('Source: global settings. Changes take effect after saving.')}
      </p>
      <h3 className='text-sm font-medium'>{t('Request text filtering')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Checks text extracted from supported requests against keywords, ignoring case. A match rejects the request before upstream processing and does not affect channel health. Images, audio and generated responses are not checked.'
        )}
      </p>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={form.formState.isSubmitting}
            saveLabel='Save sensitive words'
          />
          <Alert>
            <AlertDescription>
              {form.watch('CheckSensitiveEnabled') &&
              form.watch('CheckSensitiveOnPromptEnabled') &&
              form.watch('SensitiveWords')?.trim()
                ? t(
                    'Prompt text filtering is active with the current form values.'
                  )
                : t(
                    'Prompt text filtering needs both switches enabled and a non-empty keyword list.'
                  )}
            </AlertDescription>
          </Alert>
          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='CheckSensitiveEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable filtering')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Blocks messages when sensitive keywords are detected.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <SettingsControlChildren>
              <FormField
                control={form.control}
                name='CheckSensitiveOnPromptEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Inspect user prompts')}</FormLabel>
                      <FormDescription>
                        {t(
                          'When enabled, prompts are scanned before reaching upstream models.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        disabled={!form.watch('CheckSensitiveEnabled')}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsControlChildren>
          </SettingsControlGroup>

          <FormField
            control={form.control}
            name='target.scope'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Filter scope')}</FormLabel>
                <Select
                  items={scopes}
                  value={field.value}
                  onValueChange={field.onChange}
                >
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue placeholder={t('Select')} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    {scopes.map(({ value, label }) => (
                      <SelectItem key={value} value={value}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          {(['channel', 'token'] as const)
            .filter((kind) => scope === kind || scope === 'channel_token')
            .map((kind) => (
              <FormField
                key={kind}
                control={form.control}
                name={`target.${kind}_ids`}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {kind === 'channel' ? t('Channels') : t('API Keys')}
                    </FormLabel>
                    <FormControl>
                      <FilterTargetSelect
                        kind={kind}
                        ids={field.value}
                        onChange={field.onChange}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}

          <div className='grid gap-2'>
            <h4 className='text-sm font-medium'>{t('Exemption whitelist')}</h4>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Selected channels or API keys skip prompt text filtering even when they are inside the filter scope. Matching either list is enough.'
              )}
            </p>
          </div>
          <FormField
            control={form.control}
            name='exempt.channel_ids'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Exempt channels')}</FormLabel>
                <FormControl>
                  <FilterTargetSelect
                    kind='channel'
                    ids={field.value}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='exempt.token_ids'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Exempt API keys')}</FormLabel>
                <FormControl>
                  <FilterTargetSelect
                    kind='token'
                    ids={field.value}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='SensitiveWords'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Blocked keywords')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={12}
                    placeholder={t('Enter one keyword per line')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Each line represents one keyword. Leave blank to disable the list but keep the switch states.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
