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
import { StarIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { ArrowUpRight } from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import immersiveHero from '@/assets/tokenfly-immersive-hero.webp'
import { Button } from '@/components/ui/button'

import { Reveal } from './reveal'

const providers = ['OpenAI', 'Anthropic', 'Google', 'Meta', 'DeepSeek'] as const

/** The full-bleed image is decorative; content and controls remain real HTML. */
export function SignalHero(props: {
  startUrl: '/dashboard' | '/sign-in' | '/sign-up'
  isAuthenticated: boolean
  showPricing: boolean
}) {
  const { t } = useTranslation()
  const reduceMotion = useReducedMotion()
  const heroRef = useRef<HTMLElement>(null)

  const resetParallax = useCallback(() => {
    heroRef.current?.style.setProperty('--signal-media-x', '0px')
    heroRef.current?.style.setProperty('--signal-media-y', '0px')
  }, [])

  const handlePointerMove = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (reduceMotion || event.pointerType !== 'mouse') return

      const horizontal = (event.clientX / window.innerWidth - 0.5) * -14
      const vertical = (event.clientY / window.innerHeight - 0.5) * -10
      heroRef.current?.style.setProperty('--signal-media-x', `${horizontal}px`)
      heroRef.current?.style.setProperty('--signal-media-y', `${vertical}px`)
    },
    [reduceMotion]
  )

  return (
    <section
      ref={heroRef}
      className='signal-immersive-hero'
      aria-labelledby='brand-title'
      onPointerMove={handlePointerMove}
      onPointerLeave={resetParallax}
    >
      <div className='signal-hero-media' aria-hidden='true'>
        <img src={immersiveHero} alt='' fetchPriority='high' />
      </div>

      <div className='brand-container signal-immersive-layout'>
        <div className='signal-copy-column'>
          <Reveal className='signal-rating' delay={0.08}>
            <span aria-hidden='true'>
              <HugeiconsIcon icon={StarIcon} />
            </span>
            <p>{t('One gateway. An open model ecosystem.')}</p>
          </Reveal>

          <Reveal className='signal-headline' delay={0.16}>
            <h1 id='brand-title'>
              {t('Connect models.')}
              <br />
              <span>{t('Bring ideas to life.')}</span>
            </h1>
          </Reveal>

          <Reveal className='signal-hero-aside' delay={0.24}>
            <p>
              {t(
                'One API for your AI applications. Connect models, manage keys, and keep usage in view.'
              )}
            </p>
            <div className='brand-actions'>
              <Button
                size='lg'
                className='signal-primary-cta'
                render={<Link to={props.startUrl} />}
              >
                {props.isAuthenticated
                  ? t('Go to Dashboard')
                  : t('Start building')}
                <ArrowUpRight data-icon='inline-end' aria-hidden='true' />
              </Button>
              {props.showPricing && (
                <Link className='brand-text-link' to='/pricing'>
                  {t('View Pricing')}
                  <ArrowUpRight size={18} aria-hidden='true' />
                </Link>
              )}
            </div>
          </Reveal>
        </div>

        <Reveal className='signal-compatibility' delay={0.32}>
          <span aria-hidden='true'>
            <i />
          </span>
          <p>{t('OpenAI Compatible')}</p>
        </Reveal>

        <Reveal className='signal-provider-region' delay={0.38}>
          <p>{t('Provider examples')}</p>
          <nav aria-label={t('Provider examples')}>
            {providers.map((provider) =>
              props.showPricing ? (
                <Link key={provider} to='/pricing'>
                  {provider}
                </Link>
              ) : (
                <span key={provider}>{provider}</span>
              )
            )}
          </nav>
          <small>
            {t(
              'Provider examples; available models depend on this site’s configuration.'
            )}
          </small>
        </Reveal>

        <Reveal className='signal-proof-grid' delay={0.46}>
          <div>
            <strong>40+</strong>
            <span>{t('Channels')}</span>
          </div>
          <div>
            <strong>{t('One API')}</strong>
            <span>{t('Multi-protocol Compatible')}</span>
          </div>
          <div>
            <strong>{t('Observability')}</strong>
            <span>{t('Usage at a glance')}</span>
          </div>
        </Reveal>
      </div>
    </section>
  )
}
