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
  Check,
  Copy,
  KeyRound,
  SlidersHorizontal,
  Terminal,
} from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'

const SETUP_CODE = [
  'export NEW_API_KEY="YOUR_API_KEY"',
  'export NEW_API_BASE_URL="https://api.example.com/v1"',
  `curl "$NEW_API_BASE_URL/chat/completions" \\\n  -H "Authorization: Bearer $NEW_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{\n    "model": "YOUR_MODEL_ID",\n    "messages": [\n      {"role": "user", "content": "Hello!"}\n    ]\n  }'`,
]

export function ConnectionWalkthrough() {
  const { t } = useTranslation()
  const [active, setActive] = useState(0)
  const stepsRef = useRef<HTMLOListElement>(null)
  const reduceMotion = useReducedMotion()
  const { copiedText, copyToClipboard } = useCopyToClipboard()
  const steps = [
    {
      icon: KeyRound,
      title: t('Create an API key'),
      description: t(
        'Sign in to your console and create a key for your application.'
      ),
    },
    {
      icon: SlidersHorizontal,
      title: t('Configure your client'),
      description: t(
        'Set your API key and base URL in an OpenAI-compatible SDK or application.'
      ),
    },
    {
      icon: Terminal,
      title: t('Send your first request'),
      description: t(
        'Choose an available model, send a request, and inspect usage in your console.'
      ),
    },
  ]
  const fullCode = SETUP_CODE.join('\n\n')

  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 901px)')
    let observer: IntersectionObserver | undefined
    const observeSteps = () => {
      observer?.disconnect()
      if (!desktop.matches || !stepsRef.current) return
      observer = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) {
            if (entry.isIntersecting) {
              setActive(Number((entry.target as HTMLElement).dataset.step))
            }
          }
        },
        {
          // IntersectionObserver percentage margins use width, even vertically.
          // Pixel margins keep the reading band valid on wide desktop screens.
          rootMargin: `-${Math.round(window.innerHeight * 0.25)}px 0px -${Math.round(window.innerHeight * 0.45)}px 0px`,
          threshold: 0,
        }
      )
      for (const step of stepsRef.current.children) observer.observe(step)
    }
    observeSteps()
    desktop.addEventListener('change', observeSteps)
    window.addEventListener('resize', observeSteps, { passive: true })
    return () => {
      observer?.disconnect()
      desktop.removeEventListener('change', observeSteps)
      window.removeEventListener('resize', observeSteps)
    }
  }, [])

  return (
    <div className='brand-walkthrough'>
      <ol className='brand-walkthrough-steps' ref={stepsRef}>
        {steps.map((step, index) => (
          <li
            key={SETUP_CODE[index]}
            data-step={index}
            className={cn(active === index && 'is-active')}
          >
            <button
              type='button'
              className='brand-step-select'
              aria-current={active === index ? 'step' : undefined}
              onClick={() => {
                setActive(index)
                if (window.matchMedia('(min-width: 901px)').matches) {
                  stepsRef.current?.children[index].scrollIntoView({
                    behavior: reduceMotion ? 'instant' : 'smooth',
                    block: 'center',
                  })
                }
              }}
            >
              <span className='brand-step-index'>0{index + 1}</span>
              <span>
                <step.icon size={20} aria-hidden='true' />
                <strong>{step.title}</strong>
              </span>
            </button>
            <p>{step.description}</p>
            <pre className='brand-mobile-snippet' tabIndex={0}>
              <code>{SETUP_CODE[index]}</code>
            </pre>
          </li>
        ))}
      </ol>
      <Button
        className='brand-mobile-copy'
        variant='outline'
        onClick={() => void copyToClipboard(fullCode)}
      >
        {copiedText === fullCode ? (
          <Check aria-hidden='true' />
        ) : (
          <Copy aria-hidden='true' />
        )}
        {copiedText === fullCode ? t('Copied') : t('Copy')}
      </Button>
      <div className='brand-walkthrough-preview'>
        <div className='brand-walkthrough-toolbar'>
          <span>
            <Terminal size={17} aria-hidden='true' />
            {t('API request example')}
          </span>
          <Button
            size='sm'
            variant='ghost'
            onClick={() => void copyToClipboard(fullCode)}
          >
            {copiedText === fullCode ? (
              <Check aria-hidden='true' />
            ) : (
              <Copy aria-hidden='true' />
            )}
            {copiedText === fullCode ? t('Copied') : t('Copy')}
          </Button>
        </div>
        <div className='brand-walkthrough-stage'>
          <span>0{active + 1} / 03</span>
          <strong>{steps[active].title}</strong>
        </div>
        <pre
          className='brand-walkthrough-code'
          tabIndex={0}
          aria-label={t('API request example')}
        >
          <code>
            {SETUP_CODE.map((code, index) => (
              <span
                key={code}
                className={cn(
                  'brand-code-block',
                  active === index && 'is-active'
                )}
              >
                {code}
                {'\n\n'}
              </span>
            ))}
          </code>
        </pre>
        <p className='brand-walkthrough-note'>
          {t(
            'Illustration only. Replace the example URL, key, and model before running.'
          )}
        </p>
      </div>
    </div>
  )
}
