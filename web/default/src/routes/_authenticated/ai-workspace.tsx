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
import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { ExternalLink, Loader2, PanelsTopLeft } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { useActiveChatKey } from '@/features/chat/hooks/use-active-chat-key'
import { useChatPresets } from '@/features/chat/hooks/use-chat-presets'
import {
  chatLinkRequiresApiKey,
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
  const { chatPresets, serverAddress } = useChatPresets()
  const [isOpening, setIsOpening] = useState(false)
  const preset = useMemo(
    () =>
      chatPresets.find(
        (item) =>
          /ai\s*as\s*workspace/i.test(item.name) ||
          item.url.toLowerCase().includes('aiaw.app')
      ),
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

  const openWorkspace = useCallback(() => {
    if (!preset) return
    const url = resolveChatUrl({
      template: preset.url,
      apiKey: requiresActiveKey ? activeKey : undefined,
      serverAddress,
    })
    if (!url) return
    setIsOpening(true)
    window.open(url, '_blank', 'noopener')
    setIsOpening(false)
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
    <Main className='flex items-center justify-center p-6'>
      <div className='bg-card flex w-full max-w-2xl flex-col gap-6 rounded-xl border p-6 shadow-xs'>
        <div className='flex items-start gap-4'>
          <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg'>
            <PanelsTopLeft className='size-5' />
          </div>
          <div className='min-w-0 space-y-1'>
            <h1 className='text-xl font-semibold'>{t('AI Workspace')}</h1>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Use the external AI workspace with the models provided by this service.'
              )}
            </p>
          </div>
        </div>
        <div className='bg-muted/50 text-muted-foreground rounded-lg p-4 text-sm'>
          {t(
            'After opening, go to Provider settings, click Get Model List, select the models you need, and save.'
          )}
        </div>
        <div className='flex flex-wrap items-center gap-3'>
          <Button onClick={openWorkspace} disabled={isOpening}>
            <ExternalLink data-icon='inline-start' />
            {t('Open AI Workspace')}
          </Button>
          <Button variant='outline' render={<Link to='/playground' />}>
            {t('Back to Playground')}
          </Button>
        </div>
      </div>
    </Main>
  )
}
