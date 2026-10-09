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
import { useQuery } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  createSubscriptionAccessGroup,
  deleteSubscriptionAccessGroup,
  getSubscriptionAccessGroups,
  updateSubscriptionAccessGroup,
} from '../api'
import type { SubscriptionAccessGroup } from '../types'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SubscriptionAccessGroupsDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation()
  const [editingGroup, setEditingGroup] =
    useState<SubscriptionAccessGroup | null>(null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [deleteTarget, setDeleteTarget] =
    useState<SubscriptionAccessGroup | null>(null)

  const { data, refetch, isFetching } = useQuery({
    queryKey: ['subscription-access-groups'],
    queryFn: getSubscriptionAccessGroups,
    enabled: open,
  })

  const groups = data?.data || []

  useEffect(() => {
    if (!open) {
      setEditingGroup(null)
      setName('')
      setDescription('')
      setEnabled(true)
    }
  }, [open])

  const handleEdit = (group: SubscriptionAccessGroup) => {
    setEditingGroup(group)
    setName(group.name)
    setDescription(group.description || '')
    setEnabled(group.enabled)
  }

  const handleSubmit = async () => {
    const normalizedName = name.trim()
    if (!normalizedName) {
      toast.error(t('Subscription access group name is required'))
      return
    }
    setIsSaving(true)
    try {
      const payload = {
        name: normalizedName,
        description: description.trim(),
        enabled,
      }
      const result = editingGroup
        ? await updateSubscriptionAccessGroup(editingGroup.id, payload)
        : await createSubscriptionAccessGroup(payload)
      if (!result.success) {
        toast.error(result.message || t('Request failed'))
        return
      }
      toast.success(
        editingGroup
          ? t('Subscription access group updated')
          : t('Subscription access group created')
      )
      setEditingGroup(null)
      setName('')
      setDescription('')
      setEnabled(true)
      await refetch()
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setIsSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setIsSaving(true)
    try {
      const result = await deleteSubscriptionAccessGroup(deleteTarget.id)
      if (!result.success) {
        toast.error(result.message || t('Request failed'))
        return
      }
      toast.success(t('Subscription access group deleted'))
      if (editingGroup?.id === deleteTarget.id) {
        setEditingGroup(null)
        setName('')
        setDescription('')
        setEnabled(true)
      }
      setDeleteTarget(null)
      await refetch()
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title={t('Subscription Access Groups')}
        description={t(
          'Control which users can see and purchase restricted subscription plans.'
        )}
        contentClassName='sm:max-w-xl'
        footer={
          <Button onClick={handleSubmit} disabled={isSaving}>
            <Plus className='h-4 w-4' />
            {editingGroup ? t('Save changes') : t('Create Group')}
          </Button>
        }
      >
        <div className='space-y-4'>
          <div className='grid gap-3 sm:grid-cols-2'>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t('Subscription access group name')}
            />
            <div className='flex items-center justify-between rounded-md border px-3'>
              <span className='text-sm'>{t('Enabled')}</span>
              <Switch checked={enabled} onCheckedChange={setEnabled} />
            </div>
          </div>
          <Textarea
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder={t('Optional description')}
            rows={2}
          />
          {editingGroup ? (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => {
                setEditingGroup(null)
                setName('')
                setDescription('')
                setEnabled(true)
              }}
            >
              {t('Cancel editing')}
            </Button>
          ) : null}
          <div className='space-y-2 rounded-md border p-2'>
            {isFetching ? (
              <p className='text-muted-foreground px-2 py-3 text-sm'>
                {t('Loading...')}
              </p>
            ) : null}
            {!isFetching && groups.length === 0 ? (
              <p className='text-muted-foreground px-2 py-3 text-sm'>
                {t('No subscription access groups')}
              </p>
            ) : null}
            {groups.map((group) => (
              <div
                key={group.id}
                className='flex items-center justify-between gap-3 rounded-md px-2 py-2'
              >
                <div className='min-w-0'>
                  <p className='truncate text-sm font-medium'>{group.name}</p>
                  <p className='text-muted-foreground truncate text-xs'>
                    {group.description || t('No description')}
                  </p>
                </div>
                <div className='flex shrink-0 items-center gap-1'>
                  {!group.enabled ? (
                    <span className='text-muted-foreground text-xs'>
                      {t('Disabled')}
                    </span>
                  ) : null}
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => handleEdit(group)}
                    aria-label={t('Edit')}
                  >
                    <Pencil className='h-4 w-4' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => setDeleteTarget(group)}
                    aria-label={t('Delete')}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </div>
      </Dialog>
      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(isOpen) => !isOpen && setDeleteTarget(null)}
        title={t('Delete subscription access group')}
        desc={t(
          'Deleting this group removes it from users and subscription plans. Affected plans become visible to everyone if they have no other access groups.'
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={isSaving}
        handleConfirm={handleDelete}
      />
    </>
  )
}
