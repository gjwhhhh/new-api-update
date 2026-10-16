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
import { QueryClientProvider, focusManager } from '@tanstack/react-query'
import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it } from 'vitest'

import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'
import { useAuthStore } from '@/stores/auth-store'

import {
  userSessionAffinityQueryKey,
  type UserSessionAffinityConfig,
} from '../api'
import { UserSessionAffinityDialog } from '../user-session-affinity-dialog'
import { UserSessionAffinitySettings } from '../user-session-affinity-settings'

let client: ReturnType<typeof createAppQueryClient>
let policy: UserSessionAffinityConfig
let savedChanges: { disabled: boolean; version: number }[]
let blockNextList: boolean
let releaseOldList: (() => void) | undefined
const originalAdapter = api.defaults.adapter

function response(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, data, status: 200, statusText: 'OK', headers: {} }
}

beforeEach(() => {
  client = createAppQueryClient()
  policy = {
    user_id: 7,
    username: 'alice',
    disabled: false,
    version: 0,
    global_enabled: true,
  }
  savedChanges = []
  blockNextList = false
  releaseOldList = undefined
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  api.defaults.adapter = async (config) => {
    if (config.method === 'patch') {
      const change = JSON.parse(config.data) as {
        disabled: boolean
        version: number
      }
      savedChanges.push(change)
      if (change.version !== policy.version) {
        return response(config, {
          success: false,
          code: 'USER_SESSION_AFFINITY_CONFLICT',
        })
      }
      policy = {
        ...policy,
        disabled: change.disabled,
        version: policy.version + 1,
      }
      return response(config, { success: true, data: { ...policy } })
    }
    if (config.url === '/api/user/7/session-affinity') {
      return response(config, { success: true, data: { ...policy } })
    }
    if (config.url === '/api/user/session-affinity') {
      const disabledOnly = config.params.disabled_only as boolean
      const items = !disabledOnly || policy.disabled ? [{ ...policy }] : []
      const data = { success: true, data: { items, total: items.length } }
      if (disabledOnly && blockNextList) {
        blockNextList = false
        return new Promise((resolve) => {
          releaseOldList = () => resolve(response(config, data))
        })
      }
      return response(config, data)
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
})

afterEach(async () => {
  await act(async () => {
    cleanup()
    releaseOldList?.()
    client.clear()
  })
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.setUser(null)
  focusManager.setFocused(undefined)
})

function showSettings() {
  render(
    <QueryClientProvider client={client}>
      <UserSessionAffinitySettings globalEnabled hasGlobalChanges={false} />
    </QueryClientProvider>
  )
}

function showDialog() {
  render(
    <QueryClientProvider client={client}>
      <UserSessionAffinityDialog
        open
        user={{ id: 7, username: 'alice' }}
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

async function returnToWindow() {
  await act(async () => {
    focusManager.setFocused(false)
    focusManager.setFocused(true)
  })
}

it('refreshes the exception list on window focus even while its cached data is fresh', async () => {
  policy = { ...policy, disabled: true, version: 1 }
  showSettings()
  await screen.findByRole('button', { name: 'Edit' })

  policy = { ...policy, disabled: false, version: 2 }
  await returnToWindow()

  expect(
    await screen.findByText('All users follow system policy')
  ).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
})

it('refreshes an unedited dialog on window focus with the application query defaults', async () => {
  showDialog()
  expect(
    await screen.findByRole('radio', { name: 'Follow system policy' })
  ).toBeChecked()

  policy = { ...policy, disabled: true, version: 1 }
  await returnToWindow()

  await waitFor(() =>
    expect(
      screen.getByRole('radio', { name: 'Disable session affinity' })
    ).toBeChecked()
  )
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
})

it('preserves an edited dialog and detects its version conflict after a focus refresh', async () => {
  showDialog()
  await userEvent.click(
    await screen.findByRole('radio', { name: 'Disable session affinity' })
  )

  policy = { ...policy, version: 2, global_enabled: false }
  await returnToWindow()

  expect(
    await screen.findByText(/Session affinity is currently disabled globally/)
  ).toBeVisible()
  expect(
    screen.getByRole('radio', { name: 'Disable session affinity' })
  ).toBeChecked()
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(
    await screen.findByText(
      'This user configuration changed. Reload before saving again.'
    )
  ).toBeVisible()
  expect(savedChanges).toEqual([{ disabled: true, version: 0 }])
})

it('shows the saved exception even when a pre-save list request returns late', async () => {
  showSettings()
  await screen.findByText('All users follow system policy')
  blockNextList = true
  const oldRead = client.refetchQueries({
    queryKey: [...userSessionAffinityQueryKey, 'list', true],
  })
  await waitFor(() => expect(releaseOldList).toBeTypeOf('function'))

  await userEvent.click(
    screen.getByRole('button', { name: 'Add user configuration' })
  )
  const picker = await screen.findByRole('dialog', {
    name: 'Add user configuration',
  })
  await userEvent.click(
    await within(picker).findByRole('button', { name: 'Select' })
  )
  const editor = await screen.findByRole('dialog', {
    name: 'Session affinity for alice',
  })
  const save = within(editor).getByRole('button', { name: 'Save changes' })
  await waitFor(() => expect(save).toBeEnabled())
  await userEvent.click(save)
  await waitFor(() =>
    expect(savedChanges).toEqual([{ disabled: true, version: 0 }])
  )
  await act(async () => {
    releaseOldList?.()
    await oldRead
  })

  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(await screen.findByRole('button', { name: 'Edit' })).toBeVisible()
  expect(
    screen.queryByText('All users follow system policy')
  ).not.toBeInTheDocument()
})
