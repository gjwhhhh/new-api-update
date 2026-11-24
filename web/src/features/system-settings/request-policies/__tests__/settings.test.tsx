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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { Route as ModelsRoute } from '@/routes/_authenticated/system-settings/models/$section'
import { Route as OperationsRoute } from '@/routes/_authenticated/system-settings/operations/$section'
import { Route as PoliciesRoute } from '@/routes/_authenticated/system-settings/request-policies/$section'
import { Route as PolicyIndexRoute } from '@/routes/_authenticated/system-settings/request-policies/index'
import { Route as SecurityRoute } from '@/routes/_authenticated/system-settings/security/$section'
import { useAuthStore } from '@/stores/auth-store'

import { RequestPolicies } from '..'
import {
  defaultRequestPolicySettings,
  type RequestPolicySettings,
} from '../defaults'

type PolicyBeforeLoad = (context: { params: { section: string } }) => void

let queryClient: QueryClient
let settings: RequestPolicySettings
let guardMode: 'off' | 'shadow' | 'enforce' | 'error'

function optionsResponse() {
  return {
    success: true,
    data: Object.entries(settings).map(([key, value]) => ({
      key,
      value: String(value),
    })),
  }
}

async function renderPolicies(path: string) {
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const policyRoute = createRoute({
    getParentRoute: () => authenticated,
    path: '/system-settings/request-policies/$section',
    beforeLoad: PoliciesRoute.options.beforeLoad as PolicyBeforeLoad,
    component: RequestPolicies,
  })
  const routes = [
    policyRoute,
    createRoute({
      getParentRoute: () => authenticated,
      path: '/system-settings/request-policies/',
      beforeLoad: PolicyIndexRoute.options.beforeLoad as () => void,
    }),
    createRoute({
      getParentRoute: () => authenticated,
      path: '/system-settings/models/$section',
      beforeLoad: ModelsRoute.options.beforeLoad as PolicyBeforeLoad,
    }),
    createRoute({
      getParentRoute: () => authenticated,
      path: '/system-settings/security/$section',
      beforeLoad: SecurityRoute.options.beforeLoad as PolicyBeforeLoad,
    }),
    createRoute({
      getParentRoute: () => authenticated,
      path: '/system-settings/operations/$section',
      beforeLoad: OperationsRoute.options.beforeLoad as PolicyBeforeLoad,
    }),
  ]
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren(routes)]),
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await act(() => router.load())
  return router
}

beforeEach(() => {
  guardMode = 'enforce'
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  settings = {
    ...defaultRequestPolicySettings,
    RetryTimes: 2,
    AutomaticRetryStatusCodes: '429,500-503',
    AutomaticDisableChannelEnabled: true,
    AutomaticEnableChannelEnabled: true,
    'monitor_setting.auto_test_channel_enabled': true,
    CheckSensitiveEnabled: true,
    CheckSensitiveOnPromptEnabled: true,
    SensitiveWords: 'blocked',
    'channel_affinity_setting.rules': JSON.stringify([
      {
        name: 'Existing session rule',
        model_regex: ['gpt-.*'],
        path_regex: ['/v1/chat/completions'],
        key_sources: [{ type: 'request_header', key: 'X-Session-ID' }],
        ttl_seconds: 600,
        skip_retry_on_failure: true,
        include_using_group: true,
        include_model_name: false,
        include_rule_name: true,
        param_override_template: { temperature: 0 },
      },
    ]),
  }
  vi.spyOn(api, 'patch').mockResolvedValue({
    data: {
      success: true,
      data: { options: {} },
    },
  })
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/cyber/policy') {
      if (guardMode === 'error') throw new Error('Policy unavailable')
      return {
        data: {
          success: true,
          data: {
            mode: guardMode,
            scope: 'all',
            ttl_seconds: 600,
            blocked_action: 'block',
          },
        },
      }
    }
    if (url === '/api/option/') return { data: optionsResponse() }
    if (url === '/api/option/request_policy/filter_targets') {
      const params: URLSearchParams = config?.params
      const rows =
        params.get('kind') === 'token'
          ? [{ id: 82, name: 'Shared key', status: 1 }]
          : [
              { id: 79, name: 'Primary', status: 1 },
              { id: 80, name: 'Fallback', status: 1 },
            ]
      const search = params.get('q')?.toLowerCase()
      const ids = params.getAll('id')
      return {
        data: {
          success: true,
          data: rows.filter(({ id, name }) =>
            ids.length
              ? ids.includes(String(id))
              : !search ||
                name.toLowerCase().includes(search) ||
                String(id) === search.replace(/^#/, '')
          ),
        },
      }
    }
    if (url === '/api/option/request_policy') {
      return {
        data: {
          success: true,
          data: {
            options: Object.fromEntries(
              Object.entries(settings).map(([key, value]) => [
                key,
                String(value),
              ])
            ),
          },
        },
      }
    }
    return {
      data: {
        success: true,
        data: {
          enabled: false,
          total: 0,
          unknown: 0,
          by_rule_name: {},
          cache_capacity: 100000,
          cache_algo: 'lru',
        },
      },
    }
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  cleanup()
  queryClient.clear()
  useAuthStore.getState().auth.setUser(null)
})

describe('request policy settings', () => {
  it.each([
    ['retry', 'Save Changes'],
    ['health', 'Save Changes'],
    ['filtering', 'Save sensitive words'],
    ['affinity', 'Save Changes'],
  ])(
    'opening %s and saving unchanged values does not write options',
    async (section, saveLabel) => {
      await renderPolicies(`/system-settings/request-policies/${section}`)
      const save = await screen.findByRole('button', { name: saveLabel })
      expect(api.put).not.toHaveBeenCalled()
      await userEvent.click(save)
      await waitFor(() => expect(save).toBeEnabled())
      expect(api.put).not.toHaveBeenCalled()
    }
  )

  it.each([
    [
      'health',
      'Channel test concurrency',
      '4',
      'monitor_setting.channel_test_concurrency',
      4,
    ],
  ])(
    'saving a changed %s field writes only that original option key',
    async (section, label, input, key, value) => {
      await renderPolicies(`/system-settings/request-policies/${section}`)
      fireEvent.change(await screen.findByRole('spinbutton', { name: label }), {
        target: { value: input },
      })
      await userEvent.click(
        screen.getByRole('button', { name: 'Save Changes' })
      )
      await waitFor(() =>
        expect(api.patch).toHaveBeenCalledExactlyOnceWith(
          '/api/option/request_policy',
          { options: { [key]: String(value) } }
        )
      )
    }
  )

  it.each([false, true])(
    'saving the local ban switch from %s preserves the response mode',
    async (initial) => {
      settings.PromptRejectBanSession = initial
      settings.PromptRejectMode = 'sensitive'
      await renderPolicies('/system-settings/request-policies/filtering')
      const toggle = await screen.findByRole('switch', {
        name: 'Ban session fingerprint on local rejection',
      })
      expect(toggle).toHaveAttribute('aria-checked', String(initial))
      await userEvent.click(toggle)
      await userEvent.click(
        screen.getByRole('button', { name: 'Save sensitive words' })
      )
      await waitFor(() =>
        expect(api.patch).toHaveBeenCalledExactlyOnceWith(
          '/api/option/request_policy',
          {
            options: { PromptRejectBanSession: String(!initial) },
          }
        )
      )
    }
  )

  it('defaults to Cyber and can save the sensitive response without enabling a ban', async () => {
    await renderPolicies('/system-settings/request-policies/filtering')
    const response = await screen.findByRole('combobox', {
      name: 'Local rejection response',
    })
    expect(response).toHaveTextContent('Cyber policy error (403)')
    await userEvent.click(response)
    await userEvent.click(
      await screen.findByRole('option', { name: 'Sensitive word error' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: { PromptRejectMode: 'sensitive' },
        }
      )
    )
  })

  it.each([
    [
      'off',
      'Session protection is disabled. Local matches will only reject the current request.',
    ],
    [
      'shadow',
      'Session protection is in observation mode. Fingerprints are recorded without enforcement.',
    ],
    [
      'enforce',
      'Session protection is enforced. Scope, duration and blocked-session action follow the Cyber policy.',
    ],
    ['error', 'Unable to load the session protection policy.'],
  ] as const)(
    'when the guard is %s, enabling the local ban switch displays the actual status',
    async (mode, status) => {
      guardMode = mode
      await renderPolicies('/system-settings/request-policies/filtering')
      expect(screen.queryByText(status)).not.toBeInTheDocument()
      await userEvent.click(
        await screen.findByRole('switch', {
          name: 'Ban session fingerprint on local rejection',
        })
      )
      expect(await screen.findByText(status)).toBeVisible()
      expect(
        screen.getByRole('link', { name: 'Configure Cyber policy' })
      ).toHaveAttribute('href', '/usage-logs/cyber')
    }
  )

  it.each([
    ['x'.repeat(79), 'Each template must contain at least 80 characters.'],
    ['测'.repeat(2731), 'Template exceeds the selected size limit.'],
    [
      `${'x'.repeat(80)}\n---\n${'y'.repeat(80)}`,
      'Use a separate input for each template.',
    ],
  ])('rejects an invalid template before saving', async (template, message) => {
    await renderPolicies('/system-settings/request-policies/filtering')
    await userEvent.click(
      await screen.findByRole('button', { name: 'Add template' })
    )
    fireEvent.change(
      screen.getByRole('textbox', {
        name: 'Template 1',
      }),
      { target: { value: template } }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Template 1' }))
    expect(screen.getByRole('button', { name: 'Template 1' })).toHaveAttribute(
      'aria-expanded',
      'false'
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    expect(await screen.findByText(message)).toBeVisible()
    expect(screen.getByRole('button', { name: 'Template 1' })).toHaveAttribute(
      'aria-expanded',
      'true'
    )
    expect(screen.getByRole('textbox', { name: 'Template 1' })).toHaveAttribute(
      'aria-invalid',
      'true'
    )
    await waitFor(() =>
      expect(screen.getByRole('textbox', { name: 'Template 1' })).toHaveFocus()
    )
    expect(api.patch).not.toHaveBeenCalled()
  })

  it('adding templates expands and focuses separate inputs and saves the compatible format', async () => {
    await renderPolicies('/system-settings/request-policies/filtering')
    const first = '测'.repeat(80)
    const second = '界'.repeat(80)
    const add = await screen.findByRole('button', { name: 'Add template' })
    await userEvent.click(add)
    await waitFor(() =>
      expect(screen.getByRole('textbox', { name: 'Template 1' })).toHaveFocus()
    )
    fireEvent.change(screen.getByRole('textbox', { name: 'Template 1' }), {
      target: { value: first },
    })
    await userEvent.click(add)
    await waitFor(() =>
      expect(screen.getByRole('textbox', { name: 'Template 2' })).toHaveFocus()
    )
    fireEvent.change(screen.getByRole('textbox', { name: 'Template 2' }), {
      target: { value: second },
    })
    // Empty drafts do not create an empty template in the persisted configuration.
    await userEvent.click(add)
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: { PromptSimilarityTemplates: `${first}\n---\n${second}` },
        }
      )
    )
  })

  it('selecting a larger template limit accepts and persists a larger template', async () => {
    await renderPolicies('/system-settings/request-policies/filtering')
    await userEvent.click(
      await screen.findByRole('combobox', { name: 'Maximum template size' })
    )
    await userEvent.click(await screen.findByRole('option', { name: '16 KiB' }))
    await userEvent.click(screen.getByRole('button', { name: 'Add template' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Template 1' }), {
      target: { value: 'x'.repeat(8193) },
    })
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: {
            PromptSimilarityTemplateMaxBytes: '16384',
            PromptSimilarityTemplates: 'x'.repeat(8193),
          },
        }
      )
    )
  })

  it('lowering the saved limit preserves oversized text and raising it clears the error', async () => {
    const template = '测'.repeat(3000)
    settings.PromptSimilarityTemplates = template
    settings.PromptSimilarityTemplateMaxBytes = 16384
    await renderPolicies('/system-settings/request-policies/filtering')
    const limit = await screen.findByRole('combobox', {
      name: 'Maximum template size',
    })
    expect(limit).toHaveTextContent('16 KiB')
    const input = screen.getByRole('textbox', { name: 'Template 1' })
    expect(input).toHaveValue(template)
    await userEvent.click(limit)
    await userEvent.click(await screen.findByRole('option', { name: '8 KiB' }))
    expect(
      await screen.findByText('Template exceeds the selected size limit.')
    ).toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    expect(api.patch).not.toHaveBeenCalled()
    expect(input).toHaveValue(template)
    await userEvent.click(limit)
    await userEvent.click(await screen.findByRole('option', { name: '32 KiB' }))
    await waitFor(() =>
      expect(
        screen.queryByText('Template exceeds the selected size limit.')
      ).not.toBeInTheDocument()
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: { PromptSimilarityTemplateMaxBytes: '32768' },
        }
      )
    )
    expect(input).toHaveValue(template)
  })

  it('preserves edited and collapsed legacy templates when another template is removed', async () => {
    const first = 'first '.repeat(20)
    const second = 'second '.repeat(20)
    const third = 'third '.repeat(20)
    const edited = 'edited '.repeat(20).trim()
    settings.PromptSimilarityTemplates = `  ${first}\r\n  ---  \r\n${second}\r\n---\r\n${third}  `
    await renderPolicies('/system-settings/request-policies/filtering')
    expect(
      await screen.findByRole('textbox', { name: 'Template 1' })
    ).toHaveValue(first.trim())
    const secondToggle = screen.getByRole('button', { name: 'Template 2' })
    expect(secondToggle).toHaveAttribute('aria-expanded', 'false')
    secondToggle.focus()
    await userEvent.keyboard('{Enter}')
    expect(secondToggle).toHaveAttribute('aria-expanded', 'true')
    fireEvent.change(screen.getByRole('textbox', { name: 'Template 2' }), {
      target: { value: edited },
    })
    await userEvent.keyboard(' ')
    expect(secondToggle).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(
      screen.getByRole('button', { name: 'Remove template 1' })
    )
    expect(screen.getByRole('button', { name: 'Add template' })).toHaveFocus()
    const remaining = screen.getByRole('button', { name: 'Template 1' })
    expect(remaining).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(remaining)
    expect(screen.getByRole('textbox', { name: 'Template 1' })).toHaveValue(
      edited
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: {
            PromptSimilarityTemplates: `${edited}\n---\n${third.trim()}`,
          },
        }
      )
    )
  })

  it('removing the last template clears the list only after saving', async () => {
    settings.PromptSimilarityTemplates = 'existing '.repeat(20)
    await renderPolicies('/system-settings/request-policies/filtering')
    await userEvent.click(
      await screen.findByRole('button', { name: 'Remove template 1' })
    )
    expect(
      screen.queryByRole('textbox', { name: 'Template 1' })
    ).not.toBeInTheDocument()
    expect(api.patch).not.toHaveBeenCalled()
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: { PromptSimilarityTemplates: '' },
        }
      )
    )
  })

  it('turning filtering off preserves the prompt switch and keyword list', async () => {
    settings.PromptSimilarityTemplates = `  ${'existing '.repeat(20)}\r\n---\r\n${'another '.repeat(20)}  `
    await renderPolicies('/system-settings/request-policies/filtering')
    await userEvent.click(
      await screen.findByRole('switch', { name: 'Enable filtering' })
    )
    const prompt = screen.getByRole('switch', { name: 'Inspect user prompts' })
    expect(prompt).toHaveAttribute('aria-disabled', 'true')
    expect(prompt).toBeChecked()
    expect(
      screen.getByRole('textbox', { name: 'Blocked keywords' })
    ).toHaveValue('blocked')
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        { options: { CheckSensitiveEnabled: 'false' } }
      )
    )
  })

  it('refreshing another policy keeps unsaved filter text', async () => {
    await renderPolicies('/system-settings/request-policies/filtering')
    const keywords = await screen.findByRole('textbox', {
      name: 'Blocked keywords',
    })
    fireEvent.change(keywords, { target: { value: 'unsaved' } })
    settings.RetryTimes = 5
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
    })
    expect(keywords).toHaveValue('unsaved')
  })

  it('loads missing saved labels once and saves keyword edits without rewriting the scope', async () => {
    settings.PromptTextFilterTarget =
      '{ "scope": "channel", "channel_ids": [79], "token_ids": null }'
    await renderPolicies('/system-settings/request-policies/filtering')
    expect(await screen.findByText('Primary (#79)')).toBeVisible()
    const lookups = () =>
      vi
        .mocked(api.get)
        .mock.calls.flatMap(([url, config]) =>
          url === '/api/option/request_policy/filter_targets'
            ? ((config?.params as URLSearchParams | undefined)?.getAll('id') ??
              [])
            : []
        )
    expect(lookups()).toEqual(['79'])
    fireEvent.change(
      screen.getByRole('textbox', { name: 'Blocked keywords' }),
      {
        target: { value: 'changed' },
      }
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        { options: { SensitiveWords: 'changed' } }
      )
    )
    expect(lookups()).toEqual(['79'])
  })

  it('requires an explicit scope selection when the saved scope is invalid', async () => {
    settings.PromptTextFilterTarget = '{"scope":"channel","channel_ids":[]}'
    await renderPolicies('/system-settings/request-policies/filtering')
    const scope = await screen.findByRole('combobox', { name: 'Filter scope' })
    fireEvent.change(
      screen.getByRole('textbox', { name: 'Blocked keywords' }),
      {
        target: { value: 'changed' },
      }
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    expect(await screen.findByText('Required')).toBeVisible()
    expect(scope).toHaveAttribute('aria-invalid', 'true')
    expect(api.patch).not.toHaveBeenCalled()
  })

  it.each([
    ['{"channel_ids":[79]}', 'Primary (#79)', 'Exempt channels'],
    ['{"token_ids":[82]}', 'Shared key (#82)', 'Exempt API keys'],
  ])(
    'reopens and clears a single-dimension whitelist %s',
    async (value, label, selector) => {
      settings.PromptTextFilterExempt = value
      const user = userEvent.setup()
      await renderPolicies('/system-settings/request-policies/filtering')
      expect(await screen.findByText(label)).toBeVisible()
      await user.click(screen.getByRole('combobox', { name: selector }))
      await user.keyboard('{Backspace}{Escape}')
      expect(screen.queryByText(label)).not.toBeInTheDocument()
      await user.click(
        screen.getByRole('button', { name: 'Save sensitive words' })
      )
      await waitFor(() =>
        expect(api.patch).toHaveBeenCalledExactlyOnceWith(
          '/api/option/request_policy',
          {
            options: { PromptTextFilterExempt: '{}' },
          }
        )
      )
    }
  )

  it('adds an exemption dimension without dropping the saved channel', async () => {
    settings.PromptTextFilterExempt = '{"channel_ids":[79]}'
    const user = userEvent.setup()
    await renderPolicies('/system-settings/request-policies/filtering')
    expect(await screen.findByText('Primary (#79)')).toBeVisible()
    await user.click(screen.getByRole('combobox', { name: 'Exempt API keys' }))
    await user.click(
      await screen.findByRole('option', { name: 'Shared key (#82)' })
    )
    await user.keyboard('{Escape}')
    await user.click(
      screen.getByRole('button', { name: 'Save sensitive words' })
    )
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: {
            PromptTextFilterExempt: '{"channel_ids":[79],"token_ids":[82]}',
          },
        }
      )
    )
  })

  it('searches and saves multiple channels and a token as one scoped policy', async () => {
    const user = userEvent.setup()
    await renderPolicies('/system-settings/request-policies/filtering')
    await user.click(
      await screen.findByRole('combobox', { name: 'Filter scope' })
    )
    await user.click(
      await screen.findByRole('option', {
        name: 'Selected channels and tokens',
      })
    )
    const save = screen.getByRole('button', { name: 'Save sensitive words' })
    await user.click(save)
    expect(
      await screen.findAllByText('Select at least one target')
    ).toHaveLength(2)
    expect(api.patch).not.toHaveBeenCalled()

    const channels = screen.getByRole('combobox', { name: 'Channels' })
    await user.click(channels)
    await user.type(channels, '#79')
    await user.click(
      await screen.findByRole('option', { name: 'Primary (#79)' })
    )
    await user.click(channels)
    await user.type(channels, 'fall')
    await user.click(
      await screen.findByRole('option', { name: 'Fallback (#80)' })
    )
    await user.click(screen.getByRole('combobox', { name: 'API Keys' }))
    await user.click(
      await screen.findByRole('option', { name: 'Shared key (#82)' })
    )
    expect(channels).toBeInTheDocument()
    expect(screen.getByText('Primary (#79)')).toBeVisible()
    expect(
      vi
        .mocked(api.get)
        .mock.calls.some(
          ([url, config]) =>
            url === '/api/option/request_policy/filter_targets' &&
            (config?.params as URLSearchParams | undefined)?.has('id')
        )
    ).toBe(false)
    await user.click(save)
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledExactlyOnceWith(
        '/api/option/request_policy',
        {
          options: {
            PromptTextFilterTarget:
              '{"scope":"channel_token","channel_ids":[79,80],"token_ids":[82]}',
          },
        }
      )
    )
  })

  it('invalid retry status ranges show validation and do not write options', async () => {
    await renderPolicies('/system-settings/request-policies/retry')
    const codes = await screen.findByRole('textbox', {
      name: 'Auto-retry status codes',
    })
    expect(codes).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Advanced settings' })
    ).not.toBeInTheDocument()
    fireEvent.change(codes, { target: { value: '599-500' } })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(codes).toHaveAttribute('aria-invalid', 'true')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('a rejected save keeps the edited retry value and allows another save', async () => {
    vi.mocked(api.patch).mockResolvedValue({
      data: { success: false, message: 'Save rejected' },
    })
    await renderPolicies('/system-settings/request-policies/retry')
    const retries = await screen.findByRole('spinbutton', {
      name: 'Maximum retries',
    })
    fireEvent.change(retries, { target: { value: '4' } })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
    )
    expect(retries).toHaveValue(4)
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.patch).toHaveBeenCalledTimes(2))
  })

  it('failed initial loading shows a retry action instead of editable fallback values', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('Unavailable'))
    await renderPolicies('/system-settings/request-policies/retry')
    expect(await screen.findByText('Failed to load settings')).toBeVisible()
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(
      await screen.findByRole('spinbutton', {
        name: 'Maximum retries',
      })
    ).toHaveValue(2)
    expect(api.put).not.toHaveBeenCalled()
  })

  it('turning scheduled checks off explains recovery without changing the recovery setting', async () => {
    await renderPolicies('/system-settings/request-policies/health')
    await userEvent.click(
      await screen.findByRole('switch', { name: 'Scheduled channel tests' })
    )
    expect(
      screen.getByText(
        'Scheduled recovery is off. Bulk channel tests can still re-enable automatically disabled channels.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('switch', { name: 'Re-enable on success' })
    ).toBeChecked()
    expect(api.put).not.toHaveBeenCalled()
  })

  it('the affinity cache section opens with the keyboard and keeps the existing values', async () => {
    await renderPolicies('/system-settings/request-policies/affinity')
    const toggle = await screen.findByRole('button', {
      name: 'Affinity cache settings',
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    toggle.focus()
    await userEvent.keyboard('{Enter}')
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(
      screen.getByRole('spinbutton', { name: 'Maximum cached sessions' })
    ).toHaveValue(100000)
    expect(
      screen.getByRole('spinbutton', {
        name: 'Default session lifetime (seconds)',
      })
    ).toHaveValue(3600)
    expect(api.put).not.toHaveBeenCalled()
  })

  it.each([
    ['/system-settings/models/channel-affinity', 'routing'],
    ['/system-settings/models/routing-reliability', 'routing'],
    ['/system-settings/security/sensitive-words', 'filtering'],
    ['/system-settings/operations/monitoring', 'health'],
    ['/system-settings/request-policies/', 'routing'],
    ['/system-settings/request-policies/unknown', 'routing'],
  ])('%s opens the corresponding policy page', async (path, section) => {
    const router = await renderPolicies(path)
    await waitFor(() =>
      expect(router.state.location.pathname).toBe(
        `/system-settings/request-policies/${section}`
      )
    )
    expect(api.put).not.toHaveBeenCalled()
  })
})
