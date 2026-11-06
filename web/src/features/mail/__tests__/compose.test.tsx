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
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { MailCompose } from '../components/mail-compose'
import type { MailConfiguration } from '../types'

const configuration: MailConfiguration = {
  smtp_configured: true,
  settings: {
    records_enabled: true,
    sending_enabled: true,
    batch_limit: 1000,
    interval_milliseconds: 1000,
    retention_days: 90,
  },
}
const clients: QueryClient[] = []
function renderCompose() {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'operator', role: 100 })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <MailCompose configuration={configuration} />
    </QueryClientProvider>
  )
}
function mockRecipients() {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            id: 2,
            username: 'alice',
            email: 'alice@example.com',
            group: 'default',
            status: 1,
          },
        ],
        total: 1,
      },
    },
  })
}
afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  clients.length = 0
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})

it('requires valid input and an immutable preview before confirming a send', async () => {
  mockRecipients()
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/mail/batches/preview') {
      return {
        data: {
          success: true,
          data: {
            batch: {
              id: 7,
              total: 1,
              exclusions: [],
              duplicates: 0,
              expires_at: 2000000000,
            },
            message: { subject: 'Notice', body: '<p>Hello</p>' },
            recipients: [{ email: 'alice@example.com' }],
          },
        },
      }
    }
    return { data: { success: true, data: null } }
  })
  renderCompose()
  expect(
    screen.getByRole('button', { name: 'Preview recipients and email' })
  ).toBeDisabled()
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Preview recipients and email' })
  )
  expect(await screen.findByText('Subject is required')).toBeVisible()
  expect(post).not.toHaveBeenCalled()
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Subject' }),
    ' Notice '
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email body' }),
    'Hello'
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Preview recipients and email' })
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Confirm and send' })
  )
  expect(post).not.toHaveBeenCalledWith(
    '/api/mail/batches/7/submit',
    expect.anything()
  )
  const dialog = await screen.findByRole('alertdialog')
  await userEvent.click(
    within(dialog).getByRole('button', { name: 'Send email' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/mail/batches/7/submit', {})
  )
  expect(
    post.mock.calls.filter(([url]) => url === '/api/mail/batches/7/submit')
  ).toHaveLength(1)
})

it('invalidates the preview when the email body changes', async () => {
  mockRecipients()
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        batch: {
          id: 7,
          total: 1,
          exclusions: [],
          duplicates: 0,
          expires_at: 2000000000,
        },
        message: { subject: 'Notice', body: '<p>Hello</p>' },
        recipients: [],
      },
    },
  })
  renderCompose()
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Subject' }),
    'Notice'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email body' }),
    'Hello'
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Preview recipients and email' })
  )
  expect(
    await screen.findByRole('button', { name: 'Confirm and send' })
  ).toBeEnabled()
  await userEvent.type(screen.getByRole('textbox', { name: 'Email body' }), '!')
  expect(
    screen.queryByRole('button', { name: 'Confirm and send' })
  ).not.toBeInTheDocument()
})

it('does not present a failed preview response as a sendable batch', async () => {
  mockRecipients()
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: false, message: 'No users have a valid email address' },
  })
  renderCompose()
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Subject' }),
    'Notice'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email body' }),
    'Hello'
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Preview recipients and email' })
  )
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Preview recipients and email' })
    ).toBeEnabled()
  )
  expect(
    screen.queryByRole('button', { name: 'Confirm and send' })
  ).not.toBeInTheDocument()
})
