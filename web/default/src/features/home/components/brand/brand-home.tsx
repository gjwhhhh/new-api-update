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
import { ArrowRight, ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { TokenflyBrandMark } from '@/assets/tokenfly-brand-mark'
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
          brandMark: (
            <TokenflyBrandMark className='brand-name-mark' aria-hidden='true' />
          ),
        }}
      >
        <HomeAnnouncementPopup />
        <main id='brand-main'>
          <SignalHero
            startUrl={startUrl}
            isAuthenticated={props.isAuthenticated}
            showPricing={!!pricingLink}
          />

          <section
            className='brand-container brand-ecosystem'
            id='ecosystem'
            aria-label={t('Model ecosystem')}
          >
            <Reveal>
              <p>{t('One gateway. An open model ecosystem.')}</p>
              <div
                className='brand-provider-rail'
                aria-label={t('Provider examples')}
              >
                <span>OpenAI</span>
                <span>Anthropic</span>
                <span>Google Gemini</span>
                <span>DeepSeek</span>
                <span>Qwen</span>
              </div>
              <p className='brand-caption'>
                {t(
                  'Provider examples; available models depend on this site’s configuration.'
                )}
              </p>
            </Reveal>
          </section>

          {pricingLink && (
            <section
              className='brand-container brand-section'
              id='models'
              aria-labelledby='models-title'
            >
              <Reveal className='brand-section-heading'>
                <div>
                  <p className='brand-section-number'>
                    01 / {t('Models & pricing')}
                  </p>
                  <h2 id='models-title'>
                    {t('The right model for your next idea.')}
                  </h2>
                </div>
                <Link className='brand-text-link' to='/pricing'>
                  {t('Explore all models')}
                  <ArrowUpRight size={18} aria-hidden='true' />
                </Link>
              </Reveal>
              <p className='brand-section-description'>
                {t(
                  'Compare models and pricing before you connect. Usage and billing stay visible in your console.'
                )}
              </p>
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

          <GettingStarted startUrl={startUrl} />
          <section className='brand-container brand-closing'>
            <div>
              <h2>{t('Less setup. More building.')}</h2>
              <p>{t('Give your next idea a place to start.')}</p>
            </div>
            <Button size='lg' render={<Link to={startUrl} />}>
              {props.isAuthenticated
                ? t('Go to Dashboard')
                : t('Start building')}
              <ArrowRight aria-hidden='true' />
            </Button>
          </section>
        </main>
        <Footer className='brand-footer' />
      </PublicLayout>
    </div>
  )
}
