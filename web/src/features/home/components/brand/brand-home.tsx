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
import {
  ArrowRight01Icon,
  ArrowUpRight01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import modelNetwork from '@/assets/tokenfly-model-network.jpg'
import { Footer } from '@/components/layout/components/footer'
import { PublicLayout } from '@/components/layout/components/public-layout'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { useTopNavLinks } from '@/hooks/use-top-nav-links'

import '@/styles/brand-home.css'
import '@/styles/signal-home.css'

import { HomeAnnouncementPopup } from '../home-announcement-popup'
import { GettingStarted } from './getting-started'
import { ModelCatalog } from './model-catalog'
import { Reveal } from './reveal'
import { SignalHero } from './signal-hero'

export function BrandHome(props: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const links = useTopNavLinks()
  const pricingLink = links.find((link) => link.href === '/pricing')
  let startUrl: '/dashboard' | '/sign-in' | '/sign-up' = '/sign-up'
  if (props.isAuthenticated) startUrl = '/dashboard'
  else if (status?.register_enabled === false) startUrl = '/sign-in'

  return (
    <div className='brand-home signal-home'>
      <PublicLayout
        showMainContainer={false}
        headerProps={{
          className: 'brand-home-header',
        }}
      >
        <HomeAnnouncementPopup />
        <main id='brand-main'>
          <SignalHero
            startUrl={startUrl}
            isAuthenticated={props.isAuthenticated}
            showPricing={!!pricingLink}
          />

          <div className='signal-below'>
            <section
              className='brand-container signal-ecosystem'
              id='ecosystem'
              aria-labelledby='ecosystem-title'
            >
              <Reveal className='signal-story-intro'>
                <p className='brand-section-number'>
                  01 / {t('Model ecosystem')}
                </p>
                <h2 id='ecosystem-title'>{t('Many models. One interface.')}</h2>
                <p className='brand-section-description'>
                  {t(
                    'One API for your AI applications. Connect models, manage keys, and keep usage in view.'
                  )}
                </p>
              </Reveal>

              <div className='signal-story'>
                <Reveal className='signal-story-media'>
                  <img
                    src={modelNetwork}
                    alt=''
                    loading='lazy'
                    decoding='async'
                  />
                </Reveal>
                <Reveal className='signal-story-copy' delay={0.08}>
                  <p className='signal-story-label'>{t('Model ecosystem')}</p>
                  <div className='signal-capability-list'>
                    <div>
                      <span>01</span>
                      <strong>{t('OpenAI Compatible')}</strong>
                    </div>
                    <div>
                      <span>02</span>
                      <strong>{t('Multi-protocol Compatible')}</strong>
                    </div>
                    <div>
                      <span>03</span>
                      <strong>{t('Usage at a glance')}</strong>
                    </div>
                  </div>
                  {pricingLink && (
                    <Link className='brand-text-link' to='/pricing'>
                      {t('Explore all models')}
                      <HugeiconsIcon
                        icon={ArrowUpRight01Icon}
                        aria-hidden='true'
                      />
                    </Link>
                  )}
                </Reveal>
              </div>
            </section>

            {pricingLink && (
              <section
                className='brand-container brand-section signal-models'
                id='models'
                aria-labelledby='models-title'
              >
                <Reveal className='brand-section-heading'>
                  <div>
                    <p className='brand-section-number'>
                      02 / {t('Models & pricing')}
                    </p>
                    <h2 id='models-title'>
                      {t('The right model for your next idea.')}
                    </h2>
                    <p className='brand-section-description'>
                      {t(
                        'Compare models and pricing before you connect. Usage and billing stay visible in your console.'
                      )}
                    </p>
                  </div>
                  <Link className='brand-text-link' to='/pricing'>
                    {t('Explore all models')}
                    <HugeiconsIcon
                      icon={ArrowUpRight01Icon}
                      aria-hidden='true'
                    />
                  </Link>
                </Reveal>
                {pricingLink.requiresAuth ? (
                  <div className='brand-catalog-message'>
                    <p>{t('Sign in to view model pricing.')}</p>
                    <Button
                      variant='outline'
                      render={
                        <Link to='/sign-in' search={{ redirect: '/pricing' }} />
                      }
                    >
                      {t('Sign in')}
                    </Button>
                  </div>
                ) : (
                  <ModelCatalog />
                )}
              </section>
            )}

            <GettingStarted
              startUrl={startUrl}
              hasModelSection={!!pricingLink}
            />

            <section className='brand-container brand-closing'>
              <div>
                <p className='brand-section-number'>
                  {pricingLink ? '05' : '04'} / {t('One API')}
                </p>
                <h2>{t('Less setup. More building.')}</h2>
                <p>{t('Give your next idea a place to start.')}</p>
              </div>
              <Button size='lg' render={<Link to={startUrl} />}>
                {props.isAuthenticated
                  ? t('Go to Dashboard')
                  : t('Start building')}
                <HugeiconsIcon
                  icon={ArrowRight01Icon}
                  data-icon='inline-end'
                  aria-hidden='true'
                />
              </Button>
            </section>
          </div>
        </main>
        <Footer brandVariant='home' className='brand-footer' />
      </PublicLayout>
    </div>
  )
}
