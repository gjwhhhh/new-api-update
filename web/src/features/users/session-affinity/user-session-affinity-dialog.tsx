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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  getUserSessionAffinity,
  updateUserSessionAffinity,
  userSessionAffinityQueryKey,
  type UserSessionAffinityConfig,
} from './api'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username: string }
  initialDisabled?: boolean
}

// Callers mount this dialog only while open, resetting the draft on every visit.
export function UserSessionAffinityDialog(props: Props) {
  const { t } = useTranslation()
  const id = useId()
  const client = useQueryClient()
  const [draft, setDraft] = useState<{
    original: UserSessionAffinityConfig
    disabled: boolean
  } | null>(null)
  const query = useQuery({
    queryKey: [...userSessionAffinityQueryKey, props.user.id],
    queryFn: ({ signal }) => getUserSessionAffinity(props.user.id, signal),
    enabled: props.open,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
    meta: { errorToast: false },
  })
  const original = draft?.original ?? query.data
  const disabled =
    draft?.disabled ?? (props.initialDisabled || original?.disabled || false)
  const mutation = useMutation({
    mutationFn: (policy: { disabled: boolean; version: number }) =>
      updateUserSessionAffinity(props.user.id, policy),
    onSuccess: async (policy) => {
      client.setQueryData(
        [...userSessionAffinityQueryKey, props.user.id],
        policy
      )
      await client.invalidateQueries({ queryKey: userSessionAffinityQueryKey })
      toast.success(t('Session affinity settings saved'))
      props.onOpenChange(false)
    },
    onError: (error) =>
      handleServerError(error, t('Failed to save session affinity settings')),
    meta: { errorToast: false },
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!mutation.isPending) props.onOpenChange(open)
      }}
      title={t('Session affinity for {{username}}', {
        username: props.user.username,
      })}
      description={t(
        'Applies to all API tokens and supported models for this user.'
      )}
      footer={
        <>
          <Button
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={
              !original ||
              query.isError ||
              disabled === original.disabled ||
              mutation.isPending ||
              mutation.isError
            }
            onClick={() => {
              if (original) {
                mutation.mutate({ disabled, version: original.version })
              }
            }}
          >
            {mutation.isPending && <Spinner data-icon='inline-start' />}
            {t('Save changes')}
          </Button>
        </>
      }
    >
      <p className='text-muted-foreground text-sm'>
        {props.user.username} · ID {props.user.id}
      </p>
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          description={getServerErrorMessage(query.error)}
          onRetry={() => void query.refetch()}
        />
      )}
      {original && !query.isError && (
        <>
          {!query.data?.global_enabled && (
            <Alert>
              <AlertDescription>
                {t(
                  'Session affinity is currently disabled globally. User overrides are retained when it is enabled again.'
                )}
              </AlertDescription>
            </Alert>
          )}
          <RadioGroup
            aria-label={t('Session affinity')}
            value={disabled ? 'off' : 'inherit'}
            disabled={mutation.isPending || mutation.isError}
            onValueChange={(value) => {
              setDraft({ original, disabled: value === 'off' })
              mutation.reset()
            }}
          >
            <Label
              htmlFor={`${id}-inherit`}
              className='flex cursor-pointer items-start gap-3 rounded-md border p-3'
            >
              <RadioGroupItem
                id={`${id}-inherit`}
                value='inherit'
                aria-labelledby={`${id}-inherit-label`}
                aria-describedby={`${id}-inherit-description`}
              />
              <span className='space-y-1'>
                <span id={`${id}-inherit-label`} className='block'>
                  {t('Follow system policy')}
                </span>
                <span
                  id={`${id}-inherit-description`}
                  className='text-muted-foreground block text-xs'
                >
                  {t('Use the configured global and session rules.')}
                </span>
              </span>
            </Label>
            <Label
              htmlFor={`${id}-off`}
              className='flex cursor-pointer items-start gap-3 rounded-md border p-3'
            >
              <RadioGroupItem
                id={`${id}-off`}
                value='off'
                aria-labelledby={`${id}-off-label`}
                aria-describedby={`${id}-off-description`}
              />
              <span className='space-y-1'>
                <span id={`${id}-off-label`} className='block'>
                  {t('Disable session affinity')}
                </span>
                <span
                  id={`${id}-off-description`}
                  className='text-muted-foreground block text-xs'
                >
                  {t(
                    'Select channels normally without reusing session bindings.'
                  )}
                </span>
              </span>
            </Label>
          </RadioGroup>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Saved separately and applied to new requests. Existing WebSocket connections keep their channel until reconnecting.'
            )}
          </p>
        </>
      )}
      {mutation.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            {getServerErrorMessage(mutation.error)}
            <Button
              variant='outline'
              size='sm'
              className='mt-2'
              onClick={async () => {
                const result = await query.refetch()
                if (result.isSuccess) {
                  setDraft(null)
                  mutation.reset()
                }
              }}
            >
              {t('Reload')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
    </Dialog>
  )
}
