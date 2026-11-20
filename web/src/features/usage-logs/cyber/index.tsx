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
import { Link } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CyberBans } from './CyberBans'
import { CyberEvents } from './CyberEvents'
import { CyberPolicySettings } from './CyberPolicySettings'

export function CyberLogs() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  if (!user || user.role < ROLE.ADMIN) return null
  const canWrite = user.role === ROLE.SUPER_ADMIN

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Cyber')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          nativeButton={false}
          render={
            <Link to='/usage-logs/$section' params={{ section: 'common' }} />
          }
        >
          <ArrowLeft aria-hidden />
          {t('Common Logs')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs key={user.id} defaultValue='events'>
          <TabsList className='h-auto max-w-full flex-wrap justify-start'>
            <TabsTrigger value='events'>{t('Cyber events')}</TabsTrigger>
            <TabsTrigger value='bans'>{t('Active session bans')}</TabsTrigger>
            <TabsTrigger value='policy'>{t('Cyber policy')}</TabsTrigger>
          </TabsList>
          <TabsContent value='events'>
            <CyberEvents userId={user.id} />
          </TabsContent>
          <TabsContent value='bans'>
            <CyberBans userId={user.id} canWrite={canWrite} />
          </TabsContent>
          <TabsContent value='policy'>
            <CyberPolicySettings userId={user.id} canWrite={canWrite} />
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
