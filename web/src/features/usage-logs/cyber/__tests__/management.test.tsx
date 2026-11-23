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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import type { CyberPolicy } from '../api'
import { CyberBans } from '../CyberBans'
import { CyberEvents } from '../CyberEvents'
import { CyberPolicySettings } from '../CyberPolicySettings'

const policy: CyberPolicy = {
  mode: 'shadow',
  scope: 'token',
  token_ids: [82],
  channel_ids: [],
  ttl_seconds: 600,
  match_message: false,
}

const clients: QueryClient[] = []

function renderPanel(panel: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{panel}</QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  for (const client of clients.splice(0)) client.clear()
})

it('saves all-token permanent enforcement only after the operator confirms', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: policy },
  })
  const save = vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...policy,
        mode: 'enforce',
        scope: 'all',
        ttl_seconds: 0,
        match_message: true,
      },
    },
  })
  const user = userEvent.setup()
  renderPanel(<CyberPolicySettings userId={1} canWrite />)
  await user.selectOptions(
    await screen.findByRole('combobox', { name: 'Mode' }),
    'enforce'
  )
  await user.selectOptions(
    screen.getByRole('combobox', { name: 'Scope' }),
    'all'
  )
  const ttl = screen.getByRole('spinbutton', { name: /Ban duration/ })
  await user.clear(ttl)
  await user.type(ttl, '0')
  await user.click(screen.getByRole('switch', { name: /Match cyber_policy/ }))
  await user.click(screen.getByRole('button', { name: 'Save' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(save).not.toHaveBeenCalled()
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(save).toHaveBeenCalledWith(
    '/api/cyber/policy',
    expect.objectContaining({
      mode: 'enforce',
      scope: 'all',
      ttl_seconds: 0,
      match_message: true,
    })
  )
})

it('shows the live policy without editable controls to administrators without write access', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: policy },
  })
  renderPanel(<CyberPolicySettings userId={2} canWrite={false} />)
  expect(await screen.findByRole('combobox', { name: 'Mode' })).toBeDisabled()
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
})

it('allows continuing an empty scan window and distinguishes storage errors from zero bans', async () => {
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: { success: true, data: { items: [], cursor: '15' } },
    })
    .mockRejectedValueOnce(
      new AxiosError('Request failed', undefined, undefined, undefined, {
        data: { success: false, message: 'Cyber ban storage unavailable' },
        status: 503,
        statusText: 'Unavailable',
        headers: {},
        config: { headers: new AxiosHeaders() },
      })
    )
  renderPanel(<CyberBans userId={1} canWrite />)
  await userEvent.click(await screen.findByRole('button', { name: 'Next' }))
  expect(await screen.findByText('Cyber ban storage unavailable')).toBeVisible()
  expect(screen.queryByText('No active session bans')).not.toBeInTheDocument()
})

it('separates a newly written ban from a request blocked before relay', async () => {
  const base = {
    created_at: 1795000000,
    channel_id: 3,
    token_id: 82,
    token_name: 'test',
    protocol: 'sse',
    source: 'message',
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 2,
        items: [
          {
            ...base,
            request_id: 'upstream-rejection',
            action: 'permanent',
            error_code: 'cyber_policy',
          },
          {
            ...base,
            request_id: 'subsequent-block',
            action: 'blocked',
            error_code: 'cyber_session_blocked',
          },
        ],
      },
    },
  })
  renderPanel(<CyberEvents userId={1} />)
  const eventRow = await screen.findByRole('row', {
    name: /Permanent ban recorded/,
  })
  expect(
    screen.getByRole('row', { name: /Request blocked locally/ })
  ).toBeVisible()
  expect(within(eventRow).getByRole('link')).toHaveAttribute(
    'href',
    '/conversation-audit/?request_id=upstream-rejection'
  )
})

it('keeps old policies blocking and requires an enabled fallback plus explicit data-routing confirmation', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/cyber/policy'
          ? policy
          : {
              channels: [
                {
                  id: 3,
                  name: 'Fallback upstream',
                  status: 1,
                  groups: ['default'],
                },
                {
                  id: 4,
                  name: 'Disabled upstream',
                  status: 2,
                  groups: ['default'],
                },
              ],
              groups: ['default'],
            },
    },
  }))
  const save = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: policy } })
  const user = userEvent.setup()
  renderPanel(<CyberPolicySettings userId={1} canWrite />)
  const action = await screen.findByRole('combobox', {
    name: 'Action for blocked sessions',
  })
  expect(action).toHaveValue('block')
  await user.selectOptions(action, 'reroute')
  const target = await screen.findByRole('combobox', {
    name: 'Fallback channel',
  })
  expect(
    await screen.findByRole('option', { name: '#4 Disabled upstream' })
  ).toBeDisabled()
  await user.selectOptions(target, '3')
  await user.click(screen.getByRole('button', { name: 'Save' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText(/Request context will be sent/)).toBeVisible()
  expect(save).not.toHaveBeenCalled()
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(save).toHaveBeenCalledWith(
    '/api/cyber/policy',
    expect.objectContaining({
      blocked_action: 'reroute',
      fallback_channel_id: 3,
    })
  )
})

it('does not save rerouting when no fallback channels are available', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/cyber/policy' ? policy : { channels: [], groups: [] },
    },
  }))
  const save = vi.spyOn(api, 'put')
  const user = userEvent.setup()
  renderPanel(<CyberPolicySettings userId={1} canWrite />)
  await user.selectOptions(
    await screen.findByRole('combobox', {
      name: 'Action for blocked sessions',
    }),
    'reroute'
  )
  expect(await screen.findByText('No available channels')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Save' }))
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(save).not.toHaveBeenCalled()
})
