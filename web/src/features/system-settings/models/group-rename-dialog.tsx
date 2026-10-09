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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { previewGroupRename, renameGroup } from '../api'
import type { GroupRenamePreview } from '../types'

type GroupRenameDialogProps = {
  oldName: string | null
  onOpenChange: (open: boolean) => void
}

export function GroupRenameDialog(props: GroupRenameDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [newName, setNewName] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [preview, setPreview] = useState<GroupRenamePreview | null>(null)

  useEffect(() => {
    setNewName('')
    setConfirmation('')
    setPreview(null)
  }, [props.oldName])

  const previewMutation = useMutation({
    mutationFn: previewGroupRename,
    onSuccess: (response) => {
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to preview group rename'))
        return
      }
      setPreview(response.data)
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to preview group rename'))
    },
  })

  const renameMutation = useMutation({
    mutationFn: renameGroup,
    onSuccess: (response) => {
      if (!response.success) {
        toast.error(response.message || t('Failed to rename group'))
        return
      }
      toast.success(t('Group renamed successfully'))
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      queryClient.invalidateQueries({ queryKey: ['group-config'] })
      queryClient.invalidateQueries({ queryKey: ['playground-groups'] })
      queryClient.invalidateQueries({ queryKey: ['playground-models'] })
      props.onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to rename group'))
    },
  })

  const canRename =
    preview !== null &&
    confirmation === props.oldName &&
    !renameMutation.isPending

  return (
    <Dialog
      open={props.oldName !== null}
      onOpenChange={props.onOpenChange}
      title={t('Rename group')}
      description={t(
        'Existing users, API keys, channels, subscriptions and active tasks will move to the new group. Historical logs remain unchanged.'
      )}
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          {preview ? (
            <Button
              disabled={!canRename}
              onClick={() => {
                if (!props.oldName) return
                renameMutation.mutate({
                  old_name: props.oldName,
                  new_name: preview.new_name,
                  revision: preview.revision,
                  confirmation,
                })
              }}
            >
              {renameMutation.isPending ? t('Renaming...') : t('Rename group')}
            </Button>
          ) : (
            <Button
              disabled={!newName.trim() || previewMutation.isPending}
              onClick={() => {
                if (!props.oldName) return
                previewMutation.mutate({
                  old_name: props.oldName,
                  new_name: newName.trim(),
                })
              }}
            >
              {previewMutation.isPending
                ? t('Checking...')
                : t('Preview impact')}
            </Button>
          )}
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label htmlFor='group-rename-new-name'>{t('New group name')}</Label>
          <Input
            id='group-rename-new-name'
            value={newName}
            disabled={preview !== null}
            onChange={(event) => setNewName(event.target.value)}
          />
        </div>

        {preview ? (
          <>
            <div className='bg-muted/30 rounded-md border p-3 text-sm'>
              <div>
                {t('Users')}: {preview.affected.users}
              </div>
              <div>
                {t('API keys')}: {preview.affected.tokens}
              </div>
              <div>
                {t('Channels')}: {preview.affected.channels}
              </div>
              <div>
                {t('Subscription plans')}: {preview.affected.subscription_plans}
              </div>
              <div>
                {t('Active subscriptions')}:{' '}
                {preview.affected.active_subscriptions}
              </div>
              <div>
                {t('Active tasks')}: {preview.affected.active_tasks}
              </div>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='group-rename-confirmation'>
                {t('Type {{name}} to confirm', { name: props.oldName })}
              </Label>
              <Input
                id='group-rename-confirmation'
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
              />
            </div>
          </>
        ) : null}
      </div>
    </Dialog>
  )
}
