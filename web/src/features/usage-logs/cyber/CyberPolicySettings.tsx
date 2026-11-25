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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Save } from 'lucide-react'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { getChannelTestFilterOptions } from '@/features/channel-test-history/api'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { fetchCyber, saveCyberPolicy, type CyberPolicy } from './api'

const ids = z
  .string()
  .trim()
  .refine(
    (s) =>
      !s ||
      (s.split(',').length <= 1024 &&
        s
          .split(',')
          .every(
            (value) =>
              /^[1-9]\d*$/.test(value.trim()) &&
              Number.isSafeInteger(Number(value))
          ))
  )
const schema = z
  .object({
    mode: z.enum(['off', 'shadow', 'enforce']),
    scope: z.enum(['all', 'token', 'channel']),
    token_ids: ids,
    channel_ids: ids,
    ttl_seconds: z.number().int(),
    match_message: z.boolean(),
    capture_full_payload: z.boolean(),
    blocked_action: z.enum(['block', 'reroute']),
    fallback_channel_id: z.number().int(),
    notify_webhook_url: z
      .string()
      .trim()
      .refine((value) => !value || URL.canParse(value)),
    notify_webhook_secret: z.string().trim().max(256),
  })
  .superRefine((value, ctx) => {
    if (value.blocked_action === 'reroute' && value.fallback_channel_id < 1) {
      ctx.addIssue({ code: 'custom', path: ['fallback_channel_id'] })
    }
    if (value.mode !== 'off') {
      if (value.scope === 'token' && !value.token_ids) {
        ctx.addIssue({ code: 'custom', path: ['token_ids'] })
      }
      if (value.scope === 'channel' && !value.channel_ids) {
        ctx.addIssue({ code: 'custom', path: ['channel_ids'] })
      }
    }
    if (value.ttl_seconds === 0 && value.mode !== 'shadow') return
    if (value.ttl_seconds < 30 || value.ttl_seconds > 3600) {
      ctx.addIssue({ code: 'custom', path: ['ttl_seconds'] })
    }
  })

export function CyberPolicySettings(props: {
  userId: number
  canWrite: boolean
}) {
  const query = useQuery({
    queryKey: ['cyber', props.userId, 'policy'],
    queryFn: ({ signal }) => fetchCyber('policy', undefined, signal),
    retry: false,
    meta: { errorToast: false },
    // Keep operator edits intact while viewing this panel.
    refetchOnWindowFocus: false,
  })
  if (query.isPending) return <LoadingState />
  if (query.isError) {
    return (
      <ErrorState
        description={getServerErrorMessage(query.error)}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <PolicyForm
      key={JSON.stringify(query.data)}
      policy={query.data}
      userId={props.userId}
      canWrite={props.canWrite}
    />
  )
}

function PolicyForm(props: {
  userId: number
  canWrite: boolean
  policy: CyberPolicy
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [pending, setPending] = useState<z.infer<typeof schema> | null>(null)
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: {
      ...props.policy,
      blocked_action: props.policy.blocked_action ?? 'block',
      capture_full_payload: props.policy.capture_full_payload ?? true,
      fallback_channel_id: props.policy.fallback_channel_id ?? 0,
      notify_webhook_url: props.policy.notify_webhook_url ?? '',
      notify_webhook_secret: '',
      token_ids: props.policy.token_ids?.join(', ') ?? '',
      channel_ids: props.policy.channel_ids?.join(', ') ?? '',
    },
  })
  const scope = form.watch('scope')
  const blockedAction = form.watch('blocked_action')
  const secretConfigured = !!props.policy.notify_webhook_secret_configured
  const channelsQuery = useQuery({
    queryKey: ['cyber', props.userId, 'fallback-channels'],
    queryFn: async () => {
      const response = await getChannelTestFilterOptions()
      return requireServerSuccess(response).data?.channels ?? []
    },
    enabled: blockedAction === 'reroute' && props.canWrite,
    retry: false,
    meta: { errorToast: false },
    refetchOnWindowFocus: false,
  })
  const channels = channelsQuery.data ?? []
  const errors = form.formState.errors
  const mutation = useMutation({
    mutationFn: (value: z.infer<typeof schema>) =>
      saveCyberPolicy({
        ...value,
        fallback_channel_id:
          value.blocked_action === 'reroute' ? value.fallback_channel_id : 0,
        token_ids: value.token_ids
          ? [...new Set(value.token_ids.split(',').map(Number))]
          : [],
        channel_ids: value.channel_ids
          ? [...new Set(value.channel_ids.split(',').map(Number))]
          : [],
      }),
    meta: { errorToast: false },
    onSuccess: (saved) => {
      form.reset({ ...form.getValues(), notify_webhook_secret: '' })
      setPending(null)
      toast.success(t('Cyber policy saved'))
      client.setQueryData(['cyber', props.userId, 'policy'], saved)
    },
  })

  return (
    <div className='max-w-2xl space-y-4 py-3'>
      <form
        onSubmit={form.handleSubmit((value) => {
          if (props.canWrite) {
            if (
              value.blocked_action === 'reroute' &&
              !channels.some(
                (channel) =>
                  channel.id === value.fallback_channel_id &&
                  channel.status === 1
              )
            ) {
              form.setError('fallback_channel_id', { type: 'validate' })
              return
            }
            mutation.reset()
            setPending(value)
          }
        })}
      >
        <fieldset
          disabled={!props.canWrite || mutation.isPending}
          className='grid gap-4 sm:grid-cols-2'
        >
          <label className='grid gap-1.5 text-sm'>
            {t('Mode')}
            <NativeSelect className='w-full' {...form.register('mode')}>
              <NativeSelectOption value='off'>
                {t('Disabled')}
              </NativeSelectOption>
              <NativeSelectOption value='shadow'>
                {t('Shadow mode')}
              </NativeSelectOption>
              <NativeSelectOption value='enforce'>
                {t('Enforce bans')}
              </NativeSelectOption>
            </NativeSelect>
          </label>
          <label className='grid gap-1.5 text-sm'>
            {t('Scope')}
            <NativeSelect className='w-full' {...form.register('scope')}>
              <NativeSelectOption value='all'>
                {t('All tokens')}
              </NativeSelectOption>
              <NativeSelectOption value='token'>
                {t('Selected tokens')}
              </NativeSelectOption>
              <NativeSelectOption value='channel'>
                {t('Selected channels')}
              </NativeSelectOption>
            </NativeSelect>
          </label>
          <label className='grid gap-1.5 text-sm'>
            {t('Action for blocked sessions')}
            <NativeSelect
              className='w-full'
              {...form.register('blocked_action')}
            >
              <NativeSelectOption value='block'>
                {t('Block request')}
              </NativeSelectOption>
              <NativeSelectOption value='reroute'>
                {t('Route to fallback channel')}
              </NativeSelectOption>
            </NativeSelect>
          </label>
          {blockedAction === 'reroute' && (
            <div className='grid gap-1.5 text-sm'>
              <label className='grid gap-1.5'>
                {t('Fallback channel')}
                <NativeSelect
                  className='w-full'
                  aria-invalid={!!errors.fallback_channel_id}
                  {...form.register('fallback_channel_id', {
                    valueAsNumber: true,
                  })}
                >
                  <NativeSelectOption value='0'>
                    {t('Select a channel')}
                  </NativeSelectOption>
                  {!channels.some(
                    (channel) => channel.id === props.policy.fallback_channel_id
                  ) &&
                    !!props.policy.fallback_channel_id && (
                      <NativeSelectOption
                        value={props.policy.fallback_channel_id}
                        disabled
                      >
                        #{props.policy.fallback_channel_id}
                      </NativeSelectOption>
                    )}
                  {channels.map((channel) => (
                    <NativeSelectOption
                      key={channel.id}
                      value={channel.id}
                      disabled={channel.status !== 1}
                    >
                      #{channel.id} {channel.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </label>
              {props.canWrite && channelsQuery.isPending && <LoadingState />}
              {channelsQuery.isError && (
                <ErrorState
                  description={getServerErrorMessage(channelsQuery.error)}
                  onRetry={() => void channelsQuery.refetch()}
                />
              )}
              {channelsQuery.isSuccess &&
                !channels.some((channel) => channel.status === 1) && (
                  <span>{t('No available channels')}</span>
                )}
              {errors.fallback_channel_id && (
                <span className='text-destructive'>
                  {t('Select an enabled fallback channel')}
                </span>
              )}
            </div>
          )}
          {scope === 'token' && (
            <label className='grid gap-1.5 text-sm sm:col-span-2'>
              {t('Token IDs')}
              <Input
                placeholder='1, 2, 3'
                aria-invalid={!!errors.token_ids}
                {...form.register('token_ids')}
              />
              {errors.token_ids && (
                <span className='text-destructive'>
                  {t('Enter a comma-separated list of positive IDs')}
                </span>
              )}
            </label>
          )}
          {scope === 'channel' && (
            <label className='grid gap-1.5 text-sm sm:col-span-2'>
              {t('Channel IDs')}
              <Input
                placeholder='1, 2, 3'
                aria-invalid={!!errors.channel_ids}
                {...form.register('channel_ids')}
              />
              {errors.channel_ids && (
                <span className='text-destructive'>
                  {t('Enter a comma-separated list of positive IDs')}
                </span>
              )}
            </label>
          )}
          <label className='grid gap-1.5 text-sm'>
            {t('Ban duration (seconds)')}
            <Input
              type='number'
              min={0}
              max={3600}
              required
              aria-invalid={!!errors.ttl_seconds}
              {...form.register('ttl_seconds', { valueAsNumber: true })}
            />
            <span
              className={
                errors.ttl_seconds
                  ? 'text-destructive'
                  : 'text-muted-foreground'
              }
            >
              {t('30–3600 seconds, or 0 for permanent enforcement')}
            </span>
          </label>
          <label className='flex items-center gap-3 self-start text-sm sm:pt-6'>
            <Controller
              control={form.control}
              name='match_message'
              render={({ field }) => (
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  disabled={!props.canWrite || mutation.isPending}
                />
              )}
            />
            {t('Match cyber_policy in error messages')}
          </label>
          <div className='space-y-1.5 sm:col-span-2'>
            <label className='flex items-center gap-3 text-sm'>
              <Controller
                control={form.control}
                name='capture_full_payload'
                render={({ field }) => (
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={!props.canWrite || mutation.isPending}
                  />
                )}
              />
              {t('Record full Cyber requests and responses')}
            </label>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Stores full payloads for Cyber rejections and fallback requests when conversation auditing is enabled.'
              )}
            </p>
          </div>
          <label className='grid gap-1.5 text-sm sm:col-span-2'>
            {t('Ban notification webhook URL')}
            <Input
              type='url'
              placeholder='https://'
              aria-invalid={!!errors.notify_webhook_url}
              {...form.register('notify_webhook_url')}
            />
            {errors.notify_webhook_url ? (
              <span className='text-destructive'>
                {t(
                  'Enter a valid URL, or leave blank to disable notifications'
                )}
              </span>
            ) : (
              <span className='text-muted-foreground text-xs'>
                {t(
                  'Leave blank to disable. Sends a webhook when a session ban is newly armed.'
                )}
              </span>
            )}
          </label>
          <label className='grid gap-1.5 text-sm sm:col-span-2'>
            {t('Ban notification webhook secret')}
            <Input
              type='password'
              autoComplete='new-password'
              aria-invalid={!!errors.notify_webhook_secret}
              {...form.register('notify_webhook_secret')}
            />
            {errors.notify_webhook_secret ? (
              <span className='text-destructive'>
                {t('Webhook secret must be at most 256 characters')}
              </span>
            ) : (
              <span className='text-muted-foreground text-xs'>
                {secretConfigured
                  ? t(
                      'A secret is already saved. Leave blank to keep it, or enter a new one to replace it.'
                    )
                  : t(
                      'Optional. Used for DingTalk signing or the X-Webhook-Signature header.'
                    )}
              </span>
            )}
          </label>
          {props.canWrite && (
            <Button
              className='justify-self-start'
              type='submit'
              disabled={!form.formState.isDirty}
            >
              <Save aria-hidden />
              {t('Save')}
            </Button>
          )}
        </fieldset>
      </form>
      <ConfirmDialog
        open={props.canWrite && pending !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setPending(null)
        }}
        title={t('Save Cyber policy?')}
        desc={
          pending?.blocked_action === 'reroute'
            ? t(
                'Request context will be sent to the fallback upstream. Costs, latency and model behavior may change.'
              )
            : t('The new policy applies to new requests without a restart.')
        }
        confirmText={t('Save')}
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (pending) mutation.mutate(pending)
        }}
      >
        {mutation.isError && (
          <p role='alert' className='text-destructive text-sm'>
            {getServerErrorMessage(mutation.error)}
          </p>
        )}
      </ConfirmDialog>
    </div>
  )
}
