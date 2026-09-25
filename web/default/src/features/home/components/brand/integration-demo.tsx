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
import { Check, Copy, Code2, ArrowDown } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'

// Illustrative IDs only: this demo does not make requests or imply availability.
const EXAMPLE_MODELS = [
  {
    id: 'gpt-4o',
    provider: 'OpenAI',
    label: 'GPT-4o',
    path: 'M240 0 V16 Q240 30 226 30 H94 Q80 30 80 44 V64',
  },
  {
    id: 'claude-sonnet-4-5',
    provider: 'Anthropic',
    label: 'Claude Sonnet',
    path: 'M240 0 V64',
  },
  {
    id: 'gemini-2.5-pro',
    provider: 'Google',
    label: 'Gemini 2.5',
    path: 'M240 0 V16 Q240 30 254 30 H386 Q400 30 400 44 V64',
  },
]

export function IntegrationDemo() {
  const { t } = useTranslation()
  const [model, setModel] = useState(EXAMPLE_MODELS[0].id)
  const [language, setLanguage] = useState('python')
  const { copiedText, copyToClipboard } = useCopyToClipboard()
  const snippets: Record<string, string> = {
    python: `import os\nfrom openai import OpenAI\n\nclient = OpenAI(\n    api_key=os.environ["NEW_API_KEY"],\n    base_url=os.environ["NEW_API_BASE_URL"],\n)\n\nresponse = client.chat.completions.create(\n    model="${model}",\n    messages=[{"role": "user", "content": "Hello!"}],\n)\n\nprint(response.choices[0].message.content)`,
    curl: `curl "$NEW_API_BASE_URL/chat/completions" \\\n  -H "Authorization: Bearer $NEW_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{\n    "model": "${model}",\n    "messages": [\n      {"role": "user", "content": "Hello!"}\n    ]\n  }'`,
  }
  const code = snippets[language]
  return (
    <div className='brand-playground'>
      <div
        className='brand-routing'
        role='group'
        aria-label={t('Example model')}
      >
        <p className='brand-routing-caption'>{t('Interactive routing demo')}</p>
        <div className='brand-gateway'>
          <Code2 size={20} aria-hidden='true' />
          <strong>New API</strong>
          <span>/ v1</span>
        </div>
        <svg
          className='brand-routing-lines'
          viewBox='0 0 480 64'
          preserveAspectRatio='none'
          fill='none'
          aria-hidden='true'
        >
          {EXAMPLE_MODELS.map((item) => (
            <path
              key={item.id}
              d={item.path}
              className={cn(
                'brand-routing-track',
                item.id === model && 'is-selected'
              )}
            />
          ))}
          <path
            key={model}
            d={EXAMPLE_MODELS.find((item) => item.id === model)?.path}
            className='brand-routing-packet'
            pathLength='100'
          />
        </svg>
        <div className='brand-provider-buttons'>
          {EXAMPLE_MODELS.map((item) => (
            <button
              type='button'
              key={item.id}
              aria-pressed={model === item.id}
              aria-label={item.id}
              onClick={() => setModel(item.id)}
            >
              <span>{item.provider}</span>
              <strong>{item.label}</strong>
              <span className='brand-model-indicator' aria-hidden='true' />
            </button>
          ))}
        </div>
        <p className='brand-routing-hint'>
          <ArrowDown size={13} aria-hidden='true' />
          {t('Switch a model. Keep the same API.')}
        </p>
      </div>
      <div className='brand-demo'>
        <div className='brand-demo-title'>
          <span>
            <Code2 size={17} aria-hidden='true' />
            {t('A familiar API. A new possibility.')}
          </span>
          <span className='brand-demo-dot' aria-hidden='true' />
        </div>
        <Tabs
          value={language}
          onValueChange={(value) => setLanguage(String(value))}
        >
          <div className='brand-code-toolbar'>
            <TabsList variant='line' aria-label={t('Code language')}>
              <TabsTrigger value='python'>Python</TabsTrigger>
              <TabsTrigger value='curl'>cURL</TabsTrigger>
            </TabsList>
            <Button
              variant='ghost'
              size='sm'
              onClick={() => void copyToClipboard(code)}
            >
              {copiedText === code ? (
                <Check aria-hidden='true' />
              ) : (
                <Copy aria-hidden='true' />
              )}
              {copiedText === code ? t('Copied') : t('Copy')}
            </Button>
          </div>
          {Object.entries(snippets).map(([key, snippet]) => (
            <TabsContent value={key} key={key}>
              <pre
                className='brand-code'
                tabIndex={0}
                aria-label={t('API request example')}
              >
                <code>
                  {snippet.split(model)[0]}
                  <mark key={model}>{model}</mark>
                  {snippet.split(model)[1]}
                </code>
              </pre>
            </TabsContent>
          ))}
        </Tabs>
        <div className='brand-demo-note'>
          {t(
            'Code example only. Set your key and base URL ending in /v1; choose an available model.'
          )}
        </div>
      </div>
    </div>
  )
}
