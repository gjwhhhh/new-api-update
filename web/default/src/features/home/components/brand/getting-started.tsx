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
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { useStatus } from '@/hooks/use-status'
import { useTopNavLinks } from '@/hooks/use-top-nav-links'

import { ConnectionWalkthrough } from './connection-walkthrough'
import { Reveal } from './reveal'

export function GettingStarted(props: {
  startUrl: '/dashboard' | '/sign-in' | '/sign-up'
  hasModelSection: boolean
}) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsEnabled = useTopNavLinks().some((link) => link.title === t('Docs'))
  const configuredDocs =
    typeof status?.docs_link === 'string' ? status.docs_link : ''
  const docsUrl = /^(https?:\/\/|\/(?!\/))/.test(configuredDocs)
    ? configuredDocs
    : '/docs'
  const questions = [
    {
      question: t('Can I use my existing application?'),
      answer: t(
        'Applications with a configurable OpenAI-compatible endpoint can connect. Check the model details for supported protocols and capabilities.'
      ),
    },
    {
      question: t('How is usage billed?'),
      answer: t(
        'Billing depends on the model and your group: token-based, per-request, or dynamic pricing. Review model details before calling; actual usage appears in your logs.'
      ),
    },
    {
      question: t('Where do I manage my balance and keys?'),
      answer: t(
        'Your console brings together API keys, usage logs, and your wallet. Available recharge methods depend on the site configuration.'
      ),
    },
    {
      question: t('What should I check before sending data?'),
      answer: t(
        'Review this site’s privacy policy and the upstream provider’s data terms. Keep API keys on your server and avoid sending sensitive data without authorization.'
      ),
    },
  ]

  return (
    <>
      <section
        className='brand-setup-band'
        id='quick-start'
        aria-labelledby='setup-title'
      >
        <div className='brand-container brand-section'>
          <Reveal className='brand-section-heading'>
            <div>
              <p className='brand-section-number'>
                {props.hasModelSection ? '03' : '02'} / {t('Quick start')}
              </p>
              <h2 id='setup-title'>
                {t('From an idea to your first request.')}
              </h2>
            </div>
            {docsEnabled && (
              <a
                className='brand-text-link'
                href={docsUrl}
                target={docsUrl.startsWith('http') ? '_blank' : undefined}
                rel='noopener noreferrer'
              >
                {t('Read the documentation')}
                <ArrowUpRight size={18} aria-hidden='true' />
              </a>
            )}
          </Reveal>
          <ConnectionWalkthrough />
          <div className='brand-setup-bottom'>
            <code>OpenAI SDK / cURL / Cherry Studio</code>
            <Link className='brand-text-link' to={props.startUrl}>
              {t('Connect your application')}
              <ArrowUpRight size={18} aria-hidden='true' />
            </Link>
          </div>
        </div>
      </section>
      <section
        className='brand-container brand-section brand-faq'
        aria-labelledby='faq-title'
      >
        <Reveal>
          <p className='brand-section-number'>
            {props.hasModelSection ? '04' : '03'} / {t('Before you begin')}
          </p>
          <h2 id='faq-title'>{t('A few things worth knowing.')}</h2>
          <p className='brand-section-description'>
            {t('Clear answers, before your first call.')}
          </p>
        </Reveal>
        <Accordion>
          {questions.map((item) => (
            <AccordionItem key={item.question} value={item.question}>
              <AccordionTrigger>{item.question}</AccordionTrigger>
              <AccordionContent>{item.answer}</AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </section>
    </>
  )
}
