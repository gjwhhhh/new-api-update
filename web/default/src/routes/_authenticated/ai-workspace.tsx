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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { ExternalLink, Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Main } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useActiveChatKey } from '@/features/chat/hooks/use-active-chat-key'
import { useChatPresets } from '@/features/chat/hooks/use-chat-presets'
import {
  chatLinkRequiresApiKey,
  isAIWorkspacePreset,
  resolveChatUrl,
} from '@/features/chat/lib/chat-links'
import { isSidebarModuleEnabled } from '@/lib/nav-modules'

export const Route = createFileRoute('/_authenticated/ai-workspace')({
  beforeLoad: () => {
    if (!isSidebarModuleEnabled('chat', 'ai_workspace')) {
      throw redirect({ to: '/dashboard' })
    }
  },
  component: AIWorkspacePage,
})

function AIWorkspacePage() {
  const { t } = useTranslation()
  const [isReminderOpen, setIsReminderOpen] = useState(true)
  const { chatPresets, serverAddress } = useChatPresets()
  const preset = useMemo(
    () => chatPresets.find(isAIWorkspacePreset),
    [chatPresets]
  )
  const requiresActiveKey = Boolean(
    preset && chatLinkRequiresApiKey(preset.url)
  )
  const {
    data: activeKey,
    isPending: isKeyPending,
    error: keyError,
  } = useActiveChatKey(requiresActiveKey)

  const iframeSrc = useMemo(() => {
    if (!preset || (requiresActiveKey && !activeKey)) return ''
    return resolveChatUrl({
      template: preset.url,
      apiKey: requiresActiveKey ? activeKey : undefined,
      serverAddress,
    })
  }, [activeKey, preset, requiresActiveKey, serverAddress])

  if (!preset) {
    return (
      <Main className='flex items-center justify-center p-6'>
        <Alert variant='destructive' className='max-w-xl'>
          <AlertTitle>{t('AI Workspace is not configured')}</AlertTitle>
          <AlertDescription>
            {t(
              'Please ask an administrator to configure the AI Workspace chat link.'
            )}
          </AlertDescription>
        </Alert>
      </Main>
    )
  }

  if (requiresActiveKey && isKeyPending) {
    return (
      <Main className='flex items-center justify-center p-6'>
        <div className='flex flex-col items-center gap-3'>
          <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
          <p className='text-muted-foreground text-sm'>
            {t('Preparing your chat link...')}
          </p>
        </div>
      </Main>
    )
  }

  if (requiresActiveKey && (!activeKey || keyError)) {
    return (
      <Main className='flex items-center justify-center p-6'>
        <Alert variant='destructive' className='max-w-xl'>
          <AlertTitle>{t('Unable to open AI Workspace')}</AlertTitle>
          <AlertDescription>
            {keyError instanceof Error
              ? keyError.message
              : t(
                  'Please create or enable an API key before opening AI Workspace.'
                )}
          </AlertDescription>
        </Alert>
      </Main>
    )
  }

  return (
    <Main className='flex flex-col overflow-hidden p-0'>
      <Dialog
        open={isReminderOpen}
        onOpenChange={setIsReminderOpen}
        title={t('AI Workspace')}
        description={t(
          'After opening, go to Provider settings, click Get Model List, select the models you need, and save.'
        )}
        contentClassName='sm:max-w-md'
        footer={
          <>
            <Button
              variant='outline'
              render={
                <a href={iframeSrc} target='_blank' rel='noopener noreferrer' />
              }
            >
              <ExternalLink />
              {t('Open in new tab')}
            </Button>
            <Button onClick={() => setIsReminderOpen(false)}>
              {t('Continue with embedded workspace')}
            </Button>
          </>
        }
      >
        <span className='sr-only'>{t('AI Workspace')}</span>
      </Dialog>
      {/* This administrator-configured workspace needs storage access to keep
          its provider and model settings, which an iframe sandbox blocks. */}
      {/* oxlint-disable-next-line react/iframe-missing-sandbox */}
      <iframe
        src={iframeSrc}
        key={iframeSrc}
        className='min-h-0 flex-1 border-0'
        allow='camera; microphone'
        referrerPolicy='strict-origin-when-cross-origin'
        title={t('AI Workspace')}
      />
    </Main>
  )
}
