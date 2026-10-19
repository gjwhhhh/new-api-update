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
import { MoreHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { usePortalContainer } from '@/components/ui/portal-container'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { useLogPresets } from './context'

export function LogPresetControls() {
  const { t } = useTranslation()
  const presets = useLogPresets()
  const portalContainer = usePortalContainer()
  if (!presets) return null
  const defaultId = presets.collection?.preference.default_preset_id
  const options = [
    {
      value: 'system',
      label: `${t('System default')}${defaultId == null ? ` · ${t('Default')}` : ''}`,
    },
    ...(presets.selected === 'temporary'
      ? [{ value: 'temporary', label: t('Temporary filters'), disabled: true }]
      : []),
    ...(presets.collection?.items ?? []).map((item) => ({
      value: String(item.id),
      label: `${item.name}${item.id === defaultId ? ` · ${t('Default')}` : ''}`,
    })),
  ]
  const disabled =
    presets.busy || presets.loading || Boolean(presets.error) || !presets.ready
  return (
    <div className='w-full space-y-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <Combobox
          aria-label={t('Filter preset')}
          options={options}
          value={presets.selected}
          onValueChange={(value) => {
            if (value) presets.select(value)
          }}
          disabled={disabled}
          className='h-9 w-full min-w-0 sm:w-64'
        />
        <Button
          variant='outline'
          onClick={presets.save}
          disabled={disabled || Boolean(presets.baseline && !presets.dirty)}
        >
          {t('Save filter settings')}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                aria-label={t('Manage filter presets')}
                disabled={disabled}
              />
            }
          >
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent portalContainer={portalContainer}>
            <DropdownMenuItem onClick={presets.saveAs}>
              {t('Save as new preset')}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={presets.rename}
              disabled={!presets.baseline}
            >
              {t('Rename filter preset')}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={presets.setDefault}
              disabled={
                presets.selected === 'temporary' ||
                (presets.baseline?.id ?? null) === (defaultId ?? null)
              }
            >
              {t('Set as default')}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={presets.remove}
              disabled={!presets.baseline}
              className='text-destructive'
            >
              {t('Delete filter preset')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {presets.dirty && (
          <Badge variant='outline'>{t('Unsaved changes')}</Badge>
        )}
      </div>
      <p className='text-muted-foreground text-xs'>
        {t('Time ranges are not saved with filter presets.')}
      </p>
      {presets.updated && (
        <p role='status' className='text-muted-foreground text-xs'>
          {t(
            'This preset changed elsewhere. Reset to load its latest saved filters.'
          )}
        </p>
      )}
      {presets.notice && (
        <p role='status' className='text-muted-foreground text-xs'>
          {presets.notice === 'unavailable'
            ? t(
                'This preset is unavailable. Current filters can be saved as a new preset.'
              )
            : t(
                'Some saved filters are unavailable in this view and were ignored.'
              )}
        </p>
      )}
      {presets.error != null && (
        <Alert variant='destructive'>
          <AlertDescription>
            <span>
              {getServerErrorMessage(
                presets.error,
                t('Failed to load filter presets')
              )}
            </span>
            <div className='mt-2 flex flex-wrap gap-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={presets.retry}
                disabled={presets.busy}
              >
                {t('Retry')}
              </Button>
              {!presets.ready && (
                <Button
                  variant='outline'
                  size='sm'
                  onClick={presets.useSystemDefault}
                  disabled={presets.busy}
                >
                  {t('Temporarily use system defaults')}
                </Button>
              )}
            </div>
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
