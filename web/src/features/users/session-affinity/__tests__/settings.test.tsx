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
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import {
  userSessionAffinityQueryKey,
  type UserSessionAffinityConfig,
} from '../api'
import { UserSessionAffinityDialog } from '../user-session-affinity-dialog'
import { UserSessionAffinitySettings } from '../user-session-affinity-settings'

let client: QueryClient
let policy: UserSessionAffinityConfig

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  policy = {
    user_id: 7,
    username: 'alice',
    disabled: false,
    version: 0,
    global_enabled: true,
  }
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/user/7/session-affinity') {
      return { data: { success: true, data: { ...policy } } }
    }
    if (url === '/api/user/session-affinity') {
      const params = config?.params as {
        disabled_only: boolean
        keyword: string
      }
      const matches =
        (!params.disabled_only || policy.disabled) &&
        (!params.keyword ||
          params.keyword === '7' ||
          policy.username.includes(params.keyword))
      return {
        data: {
          success: true,
          data: {
            items: matches ? [{ ...policy }] : [],
            total: matches ? 1 : 0,
          },
        },
      }
    }
    throw new Error(`Unexpected URL: ${String(url)}`)
  })
  vi.spyOn(api, 'patch').mockImplementation(async (_url, payload) => {
    const change = payload as { disabled: boolean; version: number }
    if (change.version !== policy.version) {
      return {
        data: { success: false, code: 'USER_SESSION_AFFINITY_CONFLICT' },
      }
    }
    policy = {
      ...policy,
      disabled: change.disabled,
      version: policy.version + 1,
    }
    return { data: { success: true, data: { ...policy } } }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})

function showDialog(initialDisabled = false) {
  const onOpenChange = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <UserSessionAffinityDialog
        open
        onOpenChange={onOpenChange}
        user={{ id: 7, username: 'alice' }}
        initialDisabled={initialDisabled}
      />
    </QueryClientProvider>
  )
  return onOpenChange
}

it('saves a new preselected disabled exception with the persisted version', async () => {
  const onOpenChange = showDialog(true)
  const save = await screen.findByRole('button', { name: 'Save changes' })
  await waitFor(() => expect(save).toBeEnabled())
  expect(
    screen.getByRole('radio', { name: 'Disable session affinity' })
  ).toBeChecked()
  await userEvent.click(save)
  await waitFor(() =>
    expect(api.patch).toHaveBeenCalledWith('/api/user/7/session-affinity', {
      disabled: true,
      version: 0,
    })
  )
  await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
})

it('restores inheritance and leaves an unchanged policy unsaveable', async () => {
  policy = { ...policy, disabled: true, version: 4, global_enabled: false }
  showDialog()
  expect(
    await screen.findByRole('radio', { name: 'Disable session affinity' })
  ).toBeChecked()
  const save = screen.getByRole('button', { name: 'Save changes' })
  expect(save).toBeDisabled()
  expect(
    screen.getByText(/Session affinity is currently disabled globally/)
  ).toBeVisible()
  await userEvent.click(
    screen.getByRole('radio', { name: 'Follow system policy' })
  )
  await userEvent.click(save)
  await waitFor(() =>
    expect(api.patch).toHaveBeenCalledWith('/api/user/7/session-affinity', {
      disabled: false,
      version: 4,
    })
  )
})

it('preserves an edited snapshot on background refresh and requires reloading after a conflict', async () => {
  showDialog()
  await userEvent.click(
    await screen.findByRole('radio', { name: 'Disable session affinity' })
  )
  policy = { ...policy, version: 2 }
  await client.refetchQueries({ queryKey: [...userSessionAffinityQueryKey, 7] })
  expect(
    screen.getByRole('radio', { name: 'Disable session affinity' })
  ).toBeChecked()
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(
    await screen.findByText(
      'This user configuration changed. Reload before saving again.'
    )
  ).toBeVisible()
  expect(api.patch).toHaveBeenCalledWith('/api/user/7/session-affinity', {
    disabled: true,
    version: 0,
  })
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
  await userEvent.click(screen.getByRole('button', { name: 'Reload' }))
  await waitFor(() =>
    expect(
      screen.getByRole('radio', { name: 'Follow system policy' })
    ).toBeChecked()
  )
  await userEvent.click(
    screen.getByRole('radio', { name: 'Disable session affinity' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() =>
    expect(api.patch).toHaveBeenLastCalledWith('/api/user/7/session-affinity', {
      disabled: true,
      version: 2,
    })
  )
})

it('manages exceptions through the settings card using the shared editor and refreshes after restore', async () => {
  render(
    <QueryClientProvider client={client}>
      <UserSessionAffinitySettings globalEnabled hasGlobalChanges={false} />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText('All users follow system policy')
  ).toBeVisible()
  await userEvent.click(
    screen.getByRole('button', { name: 'Add user configuration' })
  )
  const picker = await screen.findByRole('dialog', {
    name: 'Add user configuration',
  })
  await userEvent.type(
    within(picker).getByRole('textbox', {
      name: 'Search by user ID or username',
    }),
    'alice'
  )
  await waitFor(() =>
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/session-affinity',
      expect.objectContaining({
        params: { keyword: 'alice', disabled_only: false, p: 1, page_size: 10 },
      })
    )
  )
  await userEvent.click(
    await within(picker).findByRole('button', { name: 'Select' })
  )
  const editor = await screen.findByRole('dialog', {
    name: 'Session affinity for alice',
  })
  await waitFor(() =>
    expect(
      within(editor).getByRole('button', { name: 'Save changes' })
    ).toBeEnabled()
  )
  await userEvent.click(
    within(editor).getByRole('button', { name: 'Save changes' })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(await screen.findByRole('button', { name: 'Edit' })).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
  await userEvent.click(
    await screen.findByRole('radio', { name: 'Follow system policy' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(
    await screen.findByText('All users follow system policy')
  ).toBeVisible()
  expect(api.patch).toHaveBeenLastCalledWith('/api/user/7/session-affinity', {
    disabled: false,
    version: 1,
  })
})

it('does not expose the settings card or fetch users without channel write permission', () => {
  useAuthStore.getState().auth.setUser({
    id: 3,
    username: 'limited-admin',
    role: 10,
    permissions: { admin_permissions: { channel: { write: false } } },
  })
  render(
    <QueryClientProvider client={client}>
      <UserSessionAffinitySettings globalEnabled hasGlobalChanges={false} />
    </QueryClientProvider>
  )
  expect(screen.queryByText('User configuration')).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
})

it('shows a load failure instead of reporting an empty exception list', async () => {
  vi.mocked(api.get).mockRejectedValue(new Error('offline'))
  render(
    <QueryClientProvider client={client}>
      <UserSessionAffinitySettings globalEnabled hasGlobalChanges={false} />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText('Failed to load user configuration')
  ).toBeVisible()
  expect(
    screen.queryByText('All users follow system policy')
  ).not.toBeInTheDocument()
})
