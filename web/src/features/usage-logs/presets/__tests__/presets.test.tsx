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
import { focusManager, QueryClientProvider } from '@tanstack/react-query'
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
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'
import { useAuthStore } from '@/stores/auth-store'

import {
  UsageLogsProvider,
  useLogsViewScope,
} from '../../components/usage-logs-provider'
import { UsageLogsTable } from '../../components/usage-logs-table'
import type { LogFilterPreset, PresetPreference, PresetScope } from '../api'
import { useLogPresets } from '../context'
import { LogFilterPresetsProvider } from '../provider'

const base = '/api/user/self/log-filter-presets'
const originalAdapter = api.defaults.adapter
const originalPointerCapture = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'setPointerCapture'
)
let client: ReturnType<typeof createAppQueryClient>
let rows: LogFilterPreset[]
let defaults: Record<PresetScope, PresetPreference>
let writes: { method: string; id?: number; body: Record<string, unknown> }[]
let queries: URLSearchParams[]
let statQueries: URLSearchParams[]
let scopes: string[]
let failRead: boolean
let failWrite: boolean
let blockList: boolean
let releaseList: (() => void) | undefined

function result(config: InternalAxiosRequestConfig, data: unknown) {
  return {
    config,
    data: { success: true, data },
    status: 200,
    statusText: 'OK',
    headers: {},
  }
}
function reject(
  config: InternalAxiosRequestConfig,
  code: string,
  status = 409
): never {
  throw new AxiosError(code, undefined, config, undefined, {
    config,
    data: { success: false, code },
    status,
    statusText: 'Error',
    headers: {},
  })
}

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', {
    configurable: true,
    value: vi.fn(),
  })
  client = createAppQueryClient()
  rows = [
    {
      id: 11,
      name: 'Alpha',
      scope: 'self',
      category: 'common',
      filters: { model: 'alpha', type: '2' },
      version: 1,
      updated_at: 1,
    },
    {
      id: 12,
      name: 'Beta',
      scope: 'self',
      category: 'common',
      filters: { model: 'beta' },
      version: 1,
      updated_at: 1,
    },
    {
      id: 21,
      name: 'Admin',
      scope: 'all',
      category: 'common',
      filters: { model: 'admin' },
      version: 1,
      updated_at: 1,
    },
  ]
  defaults = {
    self: { default_preset_id: 11, version: 1 },
    all: { default_preset_id: 21, version: 1 },
  }
  writes = []
  queries = []
  statQueries = []
  scopes = []
  failRead = false
  failWrite = false
  blockList = false
  releaseList = undefined
  useAuthStore.getState().auth.setUser({ id: 7, username: 'viewer', role: 1 })
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  api.defaults.adapter = async (config) => {
    const url = config.url ?? ''
    const scope = (config.params?.scope ?? 'self') as PresetScope
    if (url.startsWith(base) || url === '/api/user/self/log-filter-default') {
      if (config.method === 'get' && failRead) {
        reject(config, 'LOG_PRESET_FAILED', 400)
      }
      const id = Number(url.slice(base.length + 1))
      const found = rows.find((row) => row.id === id && row.scope === scope)
      if (config.method === 'get') {
        scopes.push(scope)
        if (url === base) {
          const data = {
            items: rows
              .filter((row) => row.scope === scope)
              .map((row) => ({ ...row, filters: { ...row.filters } })),
            preference: { ...defaults[scope] },
          }
          if (blockList) {
            blockList = false
            return new Promise((resolve) => {
              releaseList = () => resolve(result(config, data))
            })
          }
          return result(config, data)
        }
        if (!found) reject(config, 'LOG_PRESET_NOT_FOUND', 404)
        return result(config, { ...found, filters: { ...found.filters } })
      }
      const body = JSON.parse(config.data) as Record<string, unknown>
      writes.push({ method: config.method ?? '', id, body })
      if (failWrite) reject(config, 'LOG_PRESET_FAILED', 400)
      if (url === '/api/user/self/log-filter-default') {
        if (body.version !== defaults[scope].version) {
          reject(config, 'LOG_PRESET_CONFLICT')
        }
        defaults[scope] = {
          default_preset_id: body.default_preset_id as number | null,
          version: defaults[scope].version + 1,
        }
        return result(config, { ...defaults[scope] })
      }
      if (config.method === 'post') {
        const created: LogFilterPreset = {
          id: 30 + rows.length,
          name: String(body.name),
          category: 'common',
          scope,
          filters: body.filters as LogFilterPreset['filters'],
          version: 1,
          updated_at: 2,
        }
        rows.push(created)
        return result(config, created)
      }
      if (!found) reject(config, 'LOG_PRESET_NOT_FOUND', 404)
      if (body.version !== found.version) reject(config, 'LOG_PRESET_CONFLICT')
      if (config.method === 'delete') {
        rows = rows.filter((row) => row.id !== found.id)
        if (defaults[scope].default_preset_id === found.id) {
          defaults[scope] = {
            default_preset_id: null,
            version: defaults[scope].version + 1,
          }
        }
        return result(config, { ...defaults[scope] })
      }
      const updated = {
        ...found,
        ...(body.name === undefined ? {} : { name: String(body.name) }),
        ...(body.filters === undefined
          ? {}
          : { filters: body.filters as LogFilterPreset['filters'] }),
        version: found.version + 1,
      }
      rows = rows.map((row) => (row.id === id ? updated : row))
      return result(config, updated)
    }
    if (url.startsWith('/api/log')) {
      const parsed = new URL(url, 'http://localhost')
      if (parsed.pathname.endsWith('/stat')) {
        statQueries.push(parsed.searchParams)
        return result(config, { quota: 0, rpm: 0, tpm: 0 })
      }
      queries.push(parsed.searchParams)
      return result(config, { items: [], total: 0 })
    }
    if (url === '/api/user/self/groups') return result(config, {})
    if (url === '/api/group/' || url === '/api/subscription/admin/plans') {
      return result(config, [])
    }
    if (url === '/api/subscription/self') {
      return result(config, { subscriptions: [], all_subscriptions: [] })
    }
    throw new Error(`Unexpected GET ${url}`)
  }
})

afterEach(async () => {
  await act(async () => {
    cleanup()
    releaseList?.()
    client.clear()
  })
  api.defaults.adapter = originalAdapter
  focusManager.setFocused(undefined)
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
  if (originalPointerCapture) {
    Object.defineProperty(
      HTMLElement.prototype,
      'setPointerCapture',
      originalPointerCapture
    )
  } else Reflect.deleteProperty(HTMLElement.prototype, 'setPointerCapture')
})

function ScopeSwitch() {
  const presets = useLogPresets()
  const { isAdminView } = useLogsViewScope()
  return (
    <button
      type='button'
      onClick={() => presets?.changeScope(isAdminView ? 'self' : 'all')}
    >
      Switch scope
    </button>
  )
}
function Fixture() {
  return (
    <UsageLogsProvider>
      <LogFilterPresetsProvider>
        <ScopeSwitch />
        <UsageLogsTable logCategory='common' />
      </LogFilterPresetsProvider>
    </UsageLogsProvider>
  )
}
async function show(initialEntry = '/usage-logs/common', mobile = false) {
  if (mobile) {
    const original = window.matchMedia
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      ...original(query),
      matches: query === '(max-width: 640px)',
    }))
  }
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: Fixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  if (mobile) {
    await userEvent.click(await screen.findByRole('button', { name: 'Filter' }))
  }
  await screen.findByRole('combobox', { name: 'Filter preset' })
  return router
}
async function choose(name: RegExp) {
  await userEvent.click(screen.getByRole('combobox', { name: 'Filter preset' }))
  await userEvent.click(await screen.findByRole('option', { name }))
}
async function menu(name: string) {
  await userEvent.click(
    screen.getByRole('button', { name: 'Manage filter presets' })
  )
  await userEvent.click(await screen.findByRole('menuitem', { name }))
}
async function focus() {
  await act(async () => {
    focusManager.setFocused(false)
    focusManager.setFocused(true)
  })
}

it('waits for the account default before querying logs and statistics, including pagination-only URLs', async () => {
  blockList = true
  await show('/usage-logs/common?pageSize=20')
  await waitFor(() => expect(releaseList).toBeTypeOf('function'))
  expect(queries).toHaveLength(0)
  expect(statQueries).toHaveLength(0)
  await act(async () => releaseList?.())
  await waitFor(() =>
    expect(screen.getByPlaceholderText('Model Name')).toHaveValue('alpha')
  )
  await waitFor(() => expect(queries.length).toBeGreaterThan(0))
  expect(queries.every((params) => params.get('model_name') === 'alpha')).toBe(
    true
  )
  expect(
    statQueries.every((params) => params.get('model_name') === 'alpha')
  ).toBe(true)
  expect(scopes.every((scope) => scope === 'self')).toBe(true)
})

it('uses an explicit URL as temporary filters and creates a new preset without changing the applied query', async () => {
  const router = await show('/usage-logs/common?model=from-link')
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Save filter settings' })
    ).toBeEnabled()
  )
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'saved-draft' },
  })
  await userEvent.click(
    screen.getByRole('button', { name: 'Save filter settings' })
  )
  const dialog = await screen.findByRole('dialog', {
    name: 'Save filter preset',
  })
  await userEvent.type(
    within(dialog).getByLabelText('Preset name'),
    'New preset'
  )
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Save changes' })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(writes[0].method).toBe('post')
  expect(writes[0].body.filters).toEqual({ model: 'saved-draft', type: '0' })
  expect(router.state.location.search.model).toBe('from-link')
  expect(
    queries.every((params) => params.get('model_name') === 'from-link')
  ).toBe(true)
  expect(defaults.self.default_preset_id).toBe(11)
})

it('resets empty fields to the selected preset and keeps saving separate from querying', async () => {
  const router = await show()
  await screen.findByDisplayValue('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'edited' },
  })
  await userEvent.click(
    screen.getByRole('button', { name: 'Save filter settings' })
  )
  await waitFor(() => expect(rows[0].filters.model).toBe('edited'))
  expect(router.state.location.search.model).toBe('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: '' },
  })
  const reset = screen.getByRole('button', { name: 'Reset' })
  await waitFor(() => expect(reset).toBeEnabled())
  await userEvent.click(reset)
  await waitFor(() => expect(router.state.location.search.model).toBe('edited'))
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('edited')
})

it('preserves draft and baseline version across focus refresh, then reports a stale save', async () => {
  await show()
  await screen.findByDisplayValue('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'local' },
  })
  rows[0] = { ...rows[0], version: 2, filters: { model: 'remote' } }
  await focus()
  expect(
    await screen.findByText(
      'This preset changed elsewhere. Reset to load its latest saved filters.'
    )
  ).toBeVisible()
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('local')
  await userEvent.click(
    screen.getByRole('button', { name: 'Save filter settings' })
  )
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(writes[0].body.version).toBe(1)
  expect(rows[0].filters.model).toBe('remote')
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('local')
})

it('keeps the selected preset when save-before-switch fails, and allows discarding afterward', async () => {
  await show()
  await screen.findByDisplayValue('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'local' },
  })
  await choose(/^Beta/)
  const confirmation = await screen.findByRole('alertdialog', {
    name: 'Unsaved changes',
  })
  failWrite = true
  await userEvent.click(
    within(confirmation).getByRole('button', { name: 'Save and switch' })
  )
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('local')
  await waitFor(() =>
    expect(
      within(confirmation).getByRole('button', { name: 'Discard and switch' })
    ).toBeEnabled()
  )
  await userEvent.click(
    within(confirmation).getByRole('button', { name: 'Discard and switch' })
  )
  await screen.findByDisplayValue('beta')
  expect(defaults.self.default_preset_id).toBe(11)
})

it('can remove the default selection without deleting presets and delete a selected preset with fallback', async () => {
  await show()
  await screen.findByDisplayValue('alpha')
  await choose(/^System default/)
  await waitFor(() =>
    expect(screen.getByPlaceholderText('Model Name')).toHaveValue('')
  )
  await menu('Set as default')
  await waitFor(() => expect(defaults.self.default_preset_id).toBeNull())
  expect(rows.filter((item) => item.scope === 'self')).toHaveLength(2)
  await choose(/^Beta/)
  await screen.findByDisplayValue('beta')
  await menu('Delete filter preset')
  const confirmation = await screen.findByRole('alertdialog', {
    name: 'Delete filter preset?',
  })
  await userEvent.click(
    within(confirmation).getByRole('button', { name: 'Delete' })
  )
  await waitFor(() =>
    expect(screen.getByPlaceholderText('Model Name')).toHaveValue('')
  )
  expect(rows.some((row) => row.id === 12)).toBe(false)
})

it('turns a remotely deleted preset into temporary filters without losing the draft', async () => {
  await show()
  await screen.findByDisplayValue('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'local' },
  })
  rows = rows.filter((row) => row.id !== 11)
  defaults.self = { default_preset_id: null, version: 2 }
  await focus()
  expect(
    await screen.findByText(
      'This preset is unavailable. Current filters can be saved as a new preset.'
    )
  ).toBeVisible()
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('local')
  await userEvent.click(
    screen.getByRole('button', { name: 'Save filter settings' })
  )
  expect(
    await screen.findByRole('dialog', { name: 'Save filter preset' })
  ).toBeVisible()
})

it('shows load failures and only queries system defaults after an explicit fallback choice', async () => {
  failRead = true
  await show()
  expect(
    await screen.findByRole('button', {
      name: 'Temporarily use system defaults',
    })
  ).toBeVisible()
  expect(queries).toHaveLength(0)
  await userEvent.click(
    screen.getByRole('button', { name: 'Temporarily use system defaults' })
  )
  await waitFor(() => expect(queries.length).toBeGreaterThan(0))
  expect(writes).toHaveLength(0)
})

it('restores the target scope default instead of carrying administrator filters into self view', async () => {
  useAuthStore.getState().auth.setUser({ id: 7, username: 'admin', role: 100 })
  await show()
  await screen.findByDisplayValue('admin')
  await userEvent.click(screen.getByRole('button', { name: 'Switch scope' }))
  await screen.findByDisplayValue('alpha')
  expect(scopes).toContain('self')
  await waitFor(() => expect(queries.at(-1)?.get('model_name')).toBe('alpha'))
})

it('keeps newer catalog data when an earlier list response arrives after saving', async () => {
  await show()
  await screen.findByDisplayValue('alpha')
  blockList = true
  const oldRead = client.refetchQueries({
    queryKey: ['log-filter-presets', 7, 'common', 'self'],
  })
  await waitFor(() => expect(releaseList).toBeTypeOf('function'))
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'new-value' },
  })
  await userEvent.click(
    screen.getByRole('button', { name: 'Save filter settings' })
  )
  await waitFor(() => expect(rows[0].filters.model).toBe('new-value'))
  await act(async () => {
    releaseList?.()
    await oldRead
  })
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Save filter settings' })
    ).toBeDisabled()
  )
  expect(
    screen.queryByText(
      'This preset changed elsewhere. Reset to load its latest saved filters.'
    )
  ).not.toBeInTheDocument()
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: '' },
  })
  await userEvent.click(screen.getByRole('button', { name: 'Reset' }))
  await screen.findByDisplayValue('new-value')
})

it('renames a preset without replacing its pending filter draft', async () => {
  const router = await show()
  await screen.findByDisplayValue('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'draft' },
  })
  await menu('Rename filter preset')
  const dialog = await screen.findByRole('dialog', {
    name: 'Rename filter preset',
  })
  fireEvent.change(within(dialog).getByLabelText('Preset name'), {
    target: { value: 'Renamed' },
  })
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Save changes' })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(rows[0].name).toBe('Renamed')
  expect(rows[0].filters.model).toBe('alpha')
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('draft')
  expect(router.state.location.search.model).toBe('alpha')
})

it('keeps the mobile drawer on reset failure and supports naming a preset above it', async () => {
  await show('/usage-logs/common', true)
  await screen.findByDisplayValue('alpha')
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'mobile-draft' },
  })
  failRead = true
  await userEvent.click(screen.getByRole('button', { name: 'Reset' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Reset' })).toBeEnabled()
  )
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('mobile-draft')
  failRead = false
  await focus()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Manage filter presets' })
    ).toBeEnabled()
  )
  await menu('Save as new preset')
  const dialog = await screen.findByRole('dialog', {
    name: 'Save filter preset',
  })
  await userEvent.type(
    within(dialog).getByLabelText('Preset name'),
    'Mobile saved'
  )
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Save changes' })
  )
  await waitFor(() =>
    expect(rows.some((item) => item.name === 'Mobile saved')).toBe(true)
  )
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: 'Save filter preset' })
    ).not.toBeInTheDocument()
  )
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('mobile-draft')
})

it('preserves an edited URL draft when retrying the initial catalog read', async () => {
  failRead = true
  await show('/usage-logs/common?model=url-model')
  await screen.findByRole('button', { name: 'Retry' })
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'local-draft' },
  })
  failRead = false
  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('button', { name: 'Retry' })
    ).not.toBeInTheDocument()
  )
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('local-draft')
  expect(queries.at(-1)?.get('model_name')).toBe('url-model')
})

it('does not interrupt default restoration when Enter is pressed during loading', async () => {
  blockList = true
  await show()
  await waitFor(() => expect(releaseList).toBeTypeOf('function'))
  fireEvent.keyDown(screen.getByPlaceholderText('Model Name'), { key: 'Enter' })
  expect(queries).toHaveLength(0)
  await act(async () => releaseList?.())
  await screen.findByDisplayValue('alpha')
  await waitFor(() => expect(queries.at(-1)?.get('model_name')).toBe('alpha'))
})

it('restores the account default when focus refresh recovers an initial failure', async () => {
  failRead = true
  await show()
  await screen.findByRole('button', { name: 'Retry' })
  expect(queries).toHaveLength(0)
  failRead = false
  await focus()
  await screen.findByDisplayValue('alpha')
  await waitFor(() =>
    expect(
      screen.getByRole('combobox', { name: 'Filter preset' })
    ).toBeEnabled()
  )
  await waitFor(() => expect(queries.at(-1)?.get('model_name')).toBe('alpha'))
})

it('preserves URL drafts when focus refresh recovers an initial failure', async () => {
  failRead = true
  await show('/usage-logs/common?preset=11&model=url-model')
  await screen.findByRole('button', { name: 'Retry' })
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'unsaved' },
  })
  failRead = false
  await focus()
  await waitFor(() =>
    expect(
      screen.getByRole('combobox', { name: 'Filter preset' })
    ).toBeEnabled()
  )
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('unsaved')
  expect(queries.at(-1)?.get('model_name')).toBe('url-model')
  await userEvent.click(
    screen.getByRole('button', { name: 'Save filter settings' })
  )
  await waitFor(() => expect(rows[0].filters.model).toBe('unsaved'))
})

it.each(['Cancel', 'Discard and switch', 'Save and switch'])(
  'protects an unsaved draft during same-preset navigation with %s',
  async (action) => {
    const router = await show('/usage-logs/common?preset=11&model=alpha')
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Filter preset' })
      ).toBeEnabled()
    )
    fireEvent.change(screen.getByPlaceholderText('Model Name'), {
      target: { value: 'unsaved' },
    })
    await act(async () => {
      void router.navigate({
        to: '/usage-logs/$section',
        params: { section: 'common' },
        search: { ...router.state.location.search, model: 'other' },
      })
    })
    const dialog = await screen.findByRole('alertdialog', {
      name: 'Unsaved changes',
    })
    expect(screen.getByPlaceholderText('Model Name')).toHaveValue('unsaved')
    await userEvent.click(within(dialog).getByRole('button', { name: action }))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    if (action === 'Cancel') {
      expect(screen.getByPlaceholderText('Model Name')).toHaveValue('unsaved')
      expect(router.state.location.search.model).toBe('alpha')
    } else {
      await screen.findByDisplayValue('other')
      await waitFor(() =>
        expect(queries.at(-1)?.get('model_name')).toBe('other')
      )
    }
    expect(rows[0].filters.model).toBe(
      action === 'Save and switch' ? 'unsaved' : 'alpha'
    )
  }
)

it('preserves an unsaved draft without blocking pagination navigation', async () => {
  const router = await show('/usage-logs/common?preset=11&model=alpha')
  await waitFor(() =>
    expect(
      screen.getByRole('combobox', { name: 'Filter preset' })
    ).toBeEnabled()
  )
  fireEvent.change(screen.getByPlaceholderText('Model Name'), {
    target: { value: 'unsaved' },
  })
  await act(async () => {
    await router.navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: { ...router.state.location.search, pageSize: 20 },
    })
  })
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(screen.getByPlaceholderText('Model Name')).toHaveValue('unsaved')
  expect(router.state.location.search.pageSize).toBe(20)
})
