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

export type GroupDisplayCategory = {
  id: string
  name: string
  icon?: string
  order: number
}

export type GroupDisplayAssignment = {
  category_id: string
  order: number
}

export type GroupDisplayConfig = {
  categories: GroupDisplayCategory[]
  assignments: Record<string, GroupDisplayAssignment>
}

export type GroupDisplayOptionSource = {
  value: string
  label: string
  desc?: string
}

type AutomaticGroupDisplayLabels = {
  domesticModels: string
  otherGroups: string
}

type AutomaticProviderRule = GroupDisplayCategory & {
  keywords: string[]
}

const automaticProviderRules: AutomaticProviderRule[] = [
  {
    id: 'provider:openai',
    name: 'GPT',
    icon: 'OpenAI.Color',
    order: 100,
    keywords: [
      'openai',
      'gpt',
      'chatgpt',
      'codex',
      'o1',
      'o3',
      'o4',
      'sora',
      'dall',
      'whisper',
      'tts',
    ],
  },
  {
    id: 'provider:anthropic',
    name: 'Claude',
    icon: 'Claude.Color',
    order: 200,
    keywords: ['anthropic', 'claude'],
  },
  {
    id: 'provider:gemini',
    name: 'Gemini',
    icon: 'Gemini.Color',
    order: 300,
    keywords: ['gemini', 'gemma', 'imagen', 'veo', 'learnlm'],
  },
  {
    id: 'provider:xai',
    name: 'Grok',
    icon: 'Grok.Color',
    order: 400,
    keywords: ['xai', 'grok'],
  },
  {
    id: 'provider:domestic',
    name: '',
    icon: '',
    order: 500,
    keywords: [
      '360gpt',
      'baidu',
      'wenxin',
      'ernie',
      'baichuan',
      'deepseek',
      'doubao',
      'volcengine',
      'seedance',
      'seedream',
      'iflytek',
      'sparkdesk',
      'internlm',
      'minimax',
      'abab',
      'hailuo',
      'mimo',
      'xiaomi',
      'moonshot',
      'kimi',
      'qwen',
      'qwq',
      'qvq',
      'tongyi',
      'stepfun',
      'tencent',
      'hunyuan',
      'yi',
      'zhipu',
      'glm',
      'chatglm',
    ],
  },
  {
    id: 'provider:mistral',
    name: 'Mistral',
    icon: 'Mistral.Color',
    order: 600,
    keywords: ['mistral', 'mixtral', 'codestral', 'ministral', 'pixtral'],
  },
  {
    id: 'provider:meta',
    name: 'Meta',
    icon: 'Meta.Color',
    order: 600,
    keywords: ['meta', 'llama'],
  },
  {
    id: 'provider:perplexity',
    name: 'Perplexity',
    icon: 'Perplexity.Color',
    order: 600,
    keywords: ['perplexity', 'sonar'],
  },
  {
    id: 'provider:nvidia',
    name: 'NVIDIA',
    icon: 'Nvidia.Color',
    order: 600,
    keywords: ['nvidia', 'nemotron'],
  },
  {
    id: 'provider:microsoft',
    name: 'Microsoft',
    icon: 'Microsoft.Color',
    order: 600,
    keywords: ['microsoft', 'phi'],
  },
  {
    id: 'provider:amazon',
    name: 'Amazon',
    icon: 'Aws.Color',
    order: 600,
    keywords: ['amazon', 'nova', 'titan'],
  },
  {
    id: 'provider:cohere',
    name: 'Cohere',
    icon: 'Cohere.Color',
    order: 600,
    keywords: ['cohere', 'command'],
  },
  {
    id: 'provider:stability-ai',
    name: 'Stability AI',
    icon: 'Stability.Color',
    order: 600,
    keywords: ['stability', 'sdxl'],
  },
  {
    id: 'provider:midjourney',
    name: 'Midjourney',
    icon: 'Midjourney',
    order: 600,
    keywords: ['midjourney'],
  },
  {
    id: 'provider:kling',
    name: 'Kling',
    icon: 'Kling.Color',
    order: 600,
    keywords: ['kling'],
  },
  {
    id: 'provider:vidu',
    name: 'Vidu',
    icon: 'Vidu.Color',
    order: 600,
    keywords: ['vidu'],
  },
  {
    id: 'provider:suno',
    name: 'Suno',
    icon: 'Suno',
    order: 600,
    keywords: ['suno'],
  },
  {
    id: 'provider:wan',
    name: 'Wan',
    icon: 'Wan',
    order: 600,
    keywords: ['wan'],
  },
]

export const EMPTY_GROUP_DISPLAY_CONFIG: GroupDisplayConfig = {
  categories: [],
  assignments: {},
}

function groupIdentifierTokens(option: GroupDisplayOptionSource): string[] {
  return `${option.value} ${option.label}`
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter(Boolean)
}

function tokenMatchesKeyword(token: string, keyword: string): boolean {
  return token === keyword || (keyword.length >= 3 && token.startsWith(keyword))
}

export function isAutomaticGroupDisplayCategoryID(categoryID: string): boolean {
  return categoryID === 'other' || categoryID.startsWith('provider:')
}

export function resolveAutomaticGroupDisplayCategory(
  option: GroupDisplayOptionSource,
  labels: AutomaticGroupDisplayLabels
): GroupDisplayCategory {
  const tokens = groupIdentifierTokens(option)
  const provider = automaticProviderRules.find((rule) =>
    rule.keywords.some((keyword) =>
      tokens.some((token) => tokenMatchesKeyword(token, keyword))
    )
  )
  if (provider) {
    return {
      id: provider.id,
      name:
        provider.id === 'provider:domestic'
          ? labels.domesticModels
          : provider.name,
      icon: provider.icon,
      order: provider.order,
    }
  }
  return {
    id: 'other',
    name: labels.otherGroups,
    icon: '',
    order: 10_000,
  }
}

export function parseGroupDisplayConfig(value: string): GroupDisplayConfig {
  try {
    const parsed = JSON.parse(value) as Partial<GroupDisplayConfig>
    return {
      categories: Array.isArray(parsed.categories) ? parsed.categories : [],
      assignments:
        parsed.assignments && typeof parsed.assignments === 'object'
          ? parsed.assignments
          : {},
    }
  } catch {
    return { categories: [], assignments: {} }
  }
}

export function serializeGroupDisplayConfig(
  config: GroupDisplayConfig
): string {
  return JSON.stringify(config, null, 2)
}
