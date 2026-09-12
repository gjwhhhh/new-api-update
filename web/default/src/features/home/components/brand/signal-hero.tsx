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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { GatewayShowcase } from './gateway-showcase'
import { Reveal } from './reveal'

/** Brand artwork is decorative; content and controls remain real, accessible HTML. */
export function SignalHero(props: {
  startUrl: '/dashboard' | '/sign-in' | '/sign-up'
  isAuthenticated: boolean
  showPricing: boolean
}) {
  const { t } = useTranslation()

  return (
    <section className='signal-hero' aria-labelledby='brand-title'>
      <div className='brand-container signal-hero-layout'>
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
            <Button size='lg' render={<Link to={props.startUrl} />}>
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
        <Reveal className='signal-showcase-reveal' delay={0.32}>
          <GatewayShowcase />
        </Reveal>
      </div>
    </section>
  )
}
