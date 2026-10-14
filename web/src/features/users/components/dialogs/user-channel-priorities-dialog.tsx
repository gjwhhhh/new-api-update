/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Route } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Spinner } from '@/components/ui/spinner'
import { useDebounce } from '@/hooks/use-debounce'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getUserChannelPriorities,
  getUserChannelPriorityOptions,
  patchUserChannelPriorities,
} from '../../api'
import {
  userChannelPriorityFormSchema,
  type DraftUserChannelPriority,
  type UserChannelPriorityFormValues,
} from '../../lib/user-channel-priority-schema'
import type {
  PatchUserChannelPrioritiesPayload,
  User,
  UserChannelPriority,
  UserChannelPriorityOption,
} from '../../types'
import { UserChannelPriorityList } from './user-channel-priority-list'
import { UserChannelPriorityOptions } from './user-channel-priority-options'

const OPTIONS_PAGE_SIZE = 20

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: Pick<User, 'id' | 'username'>
}

function toDraft(item: UserChannelPriority): DraftUserChannelPriority {
  return { ...item, priorityInput: String(item.priority) }
}

export function UserChannelPrioritiesDialog(props: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initializedUserId = useRef<number | null>(null)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const debouncedSearch = useDebounce(search, 250)
  const formId = `user-channel-priorities-form-${props.user.id}`
  const form = useForm<UserChannelPriorityFormValues>({
    resolver: zodResolver(userChannelPriorityFormSchema),
    defaultValues: { items: [] },
    mode: 'onChange',
  })
  const priorityFields = useFieldArray({
    control: form.control,
    name: 'items',
  })
  const drafts = form.watch('items')

  const prioritiesQuery = useQuery({
    queryKey: ['user-channel-priorities', props.user.id],
    queryFn: () => getUserChannelPriorities(props.user.id),
    enabled: props.open,
    meta: { errorToast: false },
  })
  const optionsQuery = useQuery({
    queryKey: [
      'user-channel-priority-options',
      props.user.id,
      debouncedSearch,
      page,
    ],
    queryFn: () =>
      getUserChannelPriorityOptions(props.user.id, {
        keyword: debouncedSearch,
        p: page,
        page_size: OPTIONS_PAGE_SIZE,
      }),
    enabled: props.open,
    placeholderData: (previous) => previous,
    meta: { errorToast: false },
  })

  useEffect(() => {
    if (!props.open) {
      initializedUserId.current = null
      return
    }
    if (
      prioritiesQuery.data?.data &&
      initializedUserId.current !== props.user.id
    ) {
      form.reset({
        items: (prioritiesQuery.data.data.items ?? []).map(toDraft),
      })
      setSearch('')
      setPage(1)
      initializedUserId.current = props.user.id
    }
  }, [form, prioritiesQuery.data, props.open, props.user.id])

  useEffect(() => {
    setPage(1)
  }, [debouncedSearch])

  const originalByChannel = useMemo(() => {
    const result = new Map<number, number>()
    for (const item of prioritiesQuery.data?.data?.items ?? []) {
      result.set(item.channel_id, item.priority)
    }
    return result
  }, [prioritiesQuery.data])
  const selectedChannelIds = useMemo(
    () => new Set(drafts.map((item) => item.channel_id)),
    [drafts]
  )
  const hasChanges = useMemo(() => {
    if (drafts.length !== originalByChannel.size) return true
    return drafts.some(
      (item) =>
        originalByChannel.get(item.channel_id) !== Number(item.priorityInput)
    )
  }, [drafts, originalByChannel])

  const saveMutation = useMutation({
    mutationFn: (payload: PatchUserChannelPrioritiesPayload) =>
      patchUserChannelPriorities(props.user.id, payload),
    onSuccess: async (response) => {
      form.reset({ items: (response.data?.items ?? []).map(toDraft) })
      await queryClient.invalidateQueries({
        queryKey: ['user-channel-priorities', props.user.id],
      })
      toast.success(t('Channel priorities saved'))
      props.onOpenChange(false)
    },
    onError: (error) => {
      handleServerError(error, t('Failed to save channel priorities'))
    },
  })

  const addChannel = (option: UserChannelPriorityOption) => {
    if (selectedChannelIds.has(option.channel_id)) return
    priorityFields.append({
      user_id: props.user.id,
      ...option,
      priority: option.global_priority,
      priorityInput: String(option.global_priority),
    })
  }

  const save = (values: UserChannelPriorityFormValues) => {
    if (!hasChanges) return
    const upserts = values.items
      .filter(
        (item) =>
          originalByChannel.get(item.channel_id) !== Number(item.priorityInput)
      )
      .map((item) => ({
        channel_id: item.channel_id,
        priority: Number(item.priorityInput),
      }))
    const currentIds = new Set(values.items.map((item) => item.channel_id))
    const deletes = [...originalByChannel.keys()].filter(
      (channelId) => !currentIds.has(channelId)
    )
    saveMutation.mutate({ upserts, deletes })
  }

  const options = optionsQuery.data?.data?.items ?? []
  const optionTotal = optionsQuery.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(optionTotal / OPTIONS_PAGE_SIZE))
  const priorityErrors = drafts.map(
    (_, index) => form.formState.errors.items?.[index]?.priorityInput?.message
  )

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Channel priorities for {{username}}', {
        username: props.user.username,
      })}
      description={t(
        'Override channel priorities only for this user. Channels with the same effective priority continue to use their configured weights.'
      )}
      contentClassName='max-h-[min(92dvh,var(--dialog-available-height))] sm:max-w-4xl'
      contentHeight='min(72vh, 760px)'
      bodyClassName='flex h-full min-h-0 flex-col gap-4'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={saveMutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={formId}
            disabled={
              !hasChanges || !form.formState.isValid || saveMutation.isPending
            }
          >
            {saveMutation.isPending && <Spinner data-icon='inline-start' />}
            {t('Save changes')}
          </Button>
        </>
      }
    >
      <Alert>
        <Route aria-hidden='true' />
        <AlertTitle>{t('Routing eligibility is unchanged')}</AlertTitle>
        <AlertDescription>
          {t(
            'Overrides affect automatic channel ordering only. Group access, model support, channel status, fixed channels, and session affinity still take precedence.'
          )}
        </AlertDescription>
      </Alert>

      {prioritiesQuery.isPending && (
        <LoadingState message={t('Loading channel priorities...')} />
      )}
      {prioritiesQuery.isError && (
        <ErrorState
          className='min-h-48'
          description={t('Failed to load channel priorities')}
          onRetry={() => prioritiesQuery.refetch()}
        />
      )}
      {!prioritiesQuery.isPending && !prioritiesQuery.isError && (
        <form
          id={formId}
          className='flex min-h-0 flex-1 flex-col gap-4'
          onSubmit={form.handleSubmit(save)}
        >
          <UserChannelPriorityList
            items={drafts}
            errors={priorityErrors}
            onPriorityChange={(index, value) =>
              form.setValue(`items.${index}.priorityInput`, value, {
                shouldDirty: true,
                shouldValidate: true,
              })
            }
            onRemove={priorityFields.remove}
          />
          <Separator />
          <UserChannelPriorityOptions
            search={search}
            page={page}
            totalPages={totalPages}
            options={options}
            selectedChannelIds={selectedChannelIds}
            isPending={optionsQuery.isPending}
            isFetching={optionsQuery.isFetching}
            isError={optionsQuery.isError}
            onSearchChange={setSearch}
            onPageChange={setPage}
            onRetry={() => optionsQuery.refetch()}
            onAdd={addChannel}
          />
        </form>
      )}
    </Dialog>
  )
}
