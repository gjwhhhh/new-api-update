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
*/
import { ExternalLink, WalletCards } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import { useRechargeCenterConfig } from './hooks/use-recharge-center-config'

export function RechargeCenter() {
  const { t } = useTranslation()
  const rechargeCenter = useRechargeCenterConfig()
  const config = rechargeCenter.data

  let content: ReactNode
  if (rechargeCenter.isPending) {
    content = (
      <Card>
        <CardContent className='space-y-4 p-5'>
          <Skeleton className='h-6 w-40' />
          <Skeleton className='h-[65vh] w-full' />
        </CardContent>
      </Card>
    )
  } else if (!config) {
    content = (
      <Alert>
        <WalletCards className='size-4' aria-hidden='true' />
        <AlertTitle>{t('Recharge Center is unavailable')}</AlertTitle>
        <AlertDescription>
          {t('The administrator has not configured a recharge center.')}
        </AlertDescription>
      </Alert>
    )
  } else if (config.displayMode === 'redirect') {
    content = (
      <Card>
        <CardContent className='flex flex-col gap-4 p-5 sm:flex-row sm:items-center sm:justify-between'>
          <p className='text-muted-foreground text-sm'>
            {t('This recharge center opens in an external page.')}
          </p>
          <Button render={<a href={config.url} rel='noopener noreferrer' />}>
            <ExternalLink className='size-4' aria-hidden='true' />
            {t('Open Recharge Center')}
          </Button>
        </CardContent>
      </Card>
    )
  } else {
    content = (
      <div className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <p className='text-muted-foreground text-sm'>
            {t('If the page does not load, open it in a new tab.')}
          </p>
          <Button
            variant='outline'
            size='sm'
            render={
              <a href={config.url} target='_blank' rel='noopener noreferrer' />
            }
          >
            <ExternalLink className='size-4' aria-hidden='true' />
            {t('Open in new tab')}
          </Button>
        </div>
        <Card className='overflow-hidden'>
          {/* The administrator configures this trusted checkout page. It needs
              first-party cookie and storage access, which a sandbox blocks. */}
          {/* oxlint-disable-next-line react/iframe-missing-sandbox */}
          <iframe
            title={t('Recharge Center')}
            src={config.url}
            referrerPolicy='strict-origin-when-cross-origin'
            className='h-[70vh] min-h-[32.5rem] w-full border-0'
          />
        </Card>
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Recharge Center')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-7xl'>{content}</div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
