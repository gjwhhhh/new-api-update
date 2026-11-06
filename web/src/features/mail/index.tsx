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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getMailConfiguration } from './api'
import { MailBatches } from './components/mail-batches'
import { MailCompose } from './components/mail-compose'
import { MailRecords } from './components/mail-records'
import { MailSettings } from './components/mail-settings'

export function MailCenter() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const canRead = hasPermission(user, 'mail', 'read')
  const canWrite = hasPermission(user, 'mail', 'write')
  const canOperate = hasPermission(user, 'mail', 'operate')
  const configuration = useQuery({
    queryKey: ['mail', 'configuration'],
    queryFn: getMailConfiguration,
    enabled: canRead,
  })
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Mail Center')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {user?.role === ROLE.SUPER_ADMIN ? (
          <>
            <Button
              variant='outline'
              render={
                <Link
                  to='/system-settings/operations/$section'
                  params={{ section: 'email' }}
                />
              }
            >
              {t('SMTP Email')}
            </Button>
            <Button
              variant='outline'
              onClick={() => setSettingsOpen(true)}
              disabled={!configuration.data}
            >
              {t('Email settings')}
            </Button>
          </>
        ) : null}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        {!canRead ? (
          <ErrorState title={t('Access denied')} />
        ) : (
          <div className='min-w-0 space-y-4'>
            <Alert>
              <AlertDescription>
                {t(
                  'SMTP accepted means the mail server accepted the email. Inbox delivery and reading are not tracked. Unknown outcomes are never retried automatically.'
                )}
              </AlertDescription>
            </Alert>
            {configuration.isError ? (
              <ErrorState onRetry={() => void configuration.refetch()} />
            ) : null}
            {configuration.isPending ? <LoadingState /> : null}
            {configuration.data ? (
              <Tabs defaultValue='records'>
                <TabsList>
                  <TabsTrigger value='records'>
                    {t('Email records')}
                  </TabsTrigger>
                  <TabsTrigger value='send'>{t('Send email')}</TabsTrigger>
                </TabsList>
                <TabsContent value='records'>
                  <MailRecords />
                </TabsContent>
                <TabsContent value='send'>
                  <div className='min-w-0 space-y-6'>
                    {canWrite ? (
                      <MailCompose configuration={configuration.data} />
                    ) : null}
                    <MailBatches
                      canOperate={canOperate}
                      sendingEnabled={
                        configuration.data.settings.sending_enabled
                      }
                    />
                  </div>
                </TabsContent>
              </Tabs>
            ) : null}
          </div>
        )}
        {settingsOpen && configuration.data ? (
          <MailSettings
            settings={configuration.data.settings}
            onClose={() => setSettingsOpen(false)}
          />
        ) : null}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
