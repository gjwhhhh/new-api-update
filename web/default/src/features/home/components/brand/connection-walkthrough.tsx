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
  CommandLineIcon,
  Copy01Icon,
  Key01Icon,
  Settings02Icon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'

const SETUP_CODE = [
  'export NEW_API_KEY="YOUR_API_KEY"',
  'export NEW_API_BASE_URL="https://api.example.com/v1"',
  `curl "$NEW_API_BASE_URL/chat/completions" \\
  -H "Authorization: Bearer $NEW_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "YOUR_MODEL_ID",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'`,
]

const stepIcons = [Key01Icon, Settings02Icon, CommandLineIcon]

export function ConnectionWalkthrough() {
  const { t } = useTranslation()
  const [active, setActive] = useState(0)
  const { copiedText, copyToClipboard } = useCopyToClipboard()
  const fullCode = SETUP_CODE.join('\n\n')
  const steps = [
    {
      title: t('Create an API key'),
      description: t(
        'Sign in to your console and create a key for your application.'
      ),
    },
    {
      title: t('Configure your client'),
      description: t(
        'Set your API key and base URL in an OpenAI-compatible SDK or application.'
      ),
    },
    {
      title: t('Send your first request'),
      description: t(
        'Choose an available model, send a request, and inspect usage in your console.'
      ),
    },
  ]

  return (
    <div className='brand-walkthrough'>
      <ol className='brand-walkthrough-steps'>
        {steps.map((step, index) => (
          <li key={SETUP_CODE[index]}>
            <button
              type='button'
              className={cn(
                'brand-step-select',
                active === index && 'is-active'
              )}
              aria-pressed={active === index}
              onMouseEnter={() => setActive(index)}
              onFocus={() => setActive(index)}
              onClick={() => setActive(index)}
            >
              <span className='brand-step-topline'>
                <span className='brand-step-index'>0{index + 1}</span>
                <span className='brand-step-icon' aria-hidden='true'>
                  <HugeiconsIcon icon={stepIcons[index]} />
                </span>
              </span>
              <strong>{step.title}</strong>
              <span className='brand-step-description'>{step.description}</span>
            </button>
          </li>
        ))}
      </ol>

      <div className='brand-walkthrough-preview'>
        <div className='brand-walkthrough-toolbar'>
          <span>
            <span className='brand-terminal-dots' aria-hidden='true'>
              <i />
              <i />
              <i />
            </span>
            {t('API request example')}
          </span>
          <Button
            size='sm'
            variant='ghost'
            onClick={() => void copyToClipboard(fullCode)}
          >
            <HugeiconsIcon
              icon={copiedText === fullCode ? Tick02Icon : Copy01Icon}
              data-icon='inline-start'
              aria-hidden='true'
            />
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
                <span className='brand-code-index'>0{index + 1}</span>
                <span>{code}</span>
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
