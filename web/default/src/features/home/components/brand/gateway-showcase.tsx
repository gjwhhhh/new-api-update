import {
  Activity03Icon,
  AiNetworkIcon,
  GaugeIcon,
  Key01Icon,
  Route01Icon,
  Shield01Icon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

const showcaseTabs = [
  { id: 'connect', label: 'Quick start', icon: Key01Icon },
  { id: 'route', label: 'Models & pricing', icon: Route01Icon },
  { id: 'observe', label: 'Usage trend', icon: Activity03Icon },
  { id: 'scale', label: 'High Performance', icon: GaugeIcon },
] as const

type ShowcaseTab = (typeof showcaseTabs)[number]['id']

function ShowcasePanel(props: { activeTab: ShowcaseTab }) {
  const { t } = useTranslation()

  if (props.activeTab === 'connect') {
    return (
      <div className='signal-demo-panel' key='connect'>
        <div className='signal-demo-heading'>
          <div>
            <span>01 / API</span>
            <h3>{t('Create an API key')}</h3>
          </div>
          <HugeiconsIcon icon={Key01Icon} aria-hidden='true' />
        </div>
        <p>
          {t('Sign in to your console and create a key for your application.')}
        </p>
        <div className='signal-key-field'>
          <code>sk-live-••••••••••••4f2a</code>
          <span>{t('Enabled')}</span>
        </div>
        <div className='signal-progress'>
          <span />
        </div>
      </div>
    )
  }

  if (props.activeTab === 'route') {
    return (
      <div className='signal-demo-panel' key='route'>
        <div className='signal-demo-heading'>
          <div>
            <span>02 / ROUTING</span>
            <h3>{t('Switch a model. Keep the same API.')}</h3>
          </div>
          <HugeiconsIcon icon={AiNetworkIcon} aria-hidden='true' />
        </div>
        <div className='signal-route-list'>
          <div>
            <span>GPT-5</span>
            <strong>42%</strong>
          </div>
          <div>
            <span>Claude</span>
            <strong>31%</strong>
          </div>
          <div>
            <span>Gemini</span>
            <strong>27%</strong>
          </div>
        </div>
      </div>
    )
  }

  if (props.activeTab === 'observe') {
    return (
      <div className='signal-demo-panel' key='observe'>
        <div className='signal-demo-heading'>
          <div>
            <span>03 / INSIGHTS</span>
            <h3>{t('Recent consumption')}</h3>
          </div>
          <HugeiconsIcon icon={Activity03Icon} aria-hidden='true' />
        </div>
        <div className='signal-metrics'>
          <div>
            <span>{t('Requests')}</span>
            <strong>18,429</strong>
          </div>
          <div>
            <span>{t('Success')}</span>
            <strong>99.98%</strong>
          </div>
          <div>
            <span>{t('Latency')}</span>
            <strong>248ms</strong>
          </div>
          <div>
            <span>Tokens</span>
            <strong>4.82M</strong>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className='signal-demo-panel' key='scale'>
      <div className='signal-demo-heading'>
        <div>
          <span>04 / RELIABILITY</span>
          <h3>{t('Secure & Reliable')}</h3>
        </div>
        <HugeiconsIcon icon={Shield01Icon} aria-hidden='true' />
      </div>
      <div className='signal-check-list'>
        {[t('Load Balancing'), t('Rate Limiting'), t('Guardrails')].map(
          (item) => (
            <div key={item}>
              <span>
                <HugeiconsIcon icon={Tick02Icon} aria-hidden='true' />
              </span>
              <p>{item}</p>
              <strong>{t('Enabled')}</strong>
            </div>
          )
        )}
      </div>
    </div>
  )
}

export function GatewayShowcase() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<ShowcaseTab>('connect')

  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (mediaQuery.matches) return

    const intervalId = window.setInterval(() => {
      setActiveTab((currentTab) => {
        const currentIndex = showcaseTabs.findIndex(
          (tab) => tab.id === currentTab
        )
        return showcaseTabs[(currentIndex + 1) % showcaseTabs.length].id
      })
    }, 4000)

    return () => window.clearInterval(intervalId)
  }, [])

  return (
    <div className='signal-showcase'>
      <div className='signal-tabs' role='tablist' aria-label={t('Features')}>
        {showcaseTabs.map((tab) => {
          const isActive = activeTab === tab.id
          return (
            <button
              key={tab.id}
              type='button'
              role='tab'
              aria-selected={isActive}
              aria-controls='signal-showcase-panel'
              className={cn(isActive && 'is-active')}
              onClick={() => setActiveTab(tab.id)}
            >
              <HugeiconsIcon icon={tab.icon} aria-hidden='true' />
              <span>{t(tab.label)}</span>
            </button>
          )
        })}
      </div>
      <div className='signal-stage' id='signal-showcase-panel' role='tabpanel'>
        <div className='signal-stage-glow' aria-hidden='true' />
        <ShowcasePanel activeTab={activeTab} />
      </div>
    </div>
  )
}
