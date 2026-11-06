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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { MailRecipients } from '../components/mail-recipients'
import type { RecipientSelection } from '../types'

const clients: QueryClient[] = []
function renderRecipients() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  function Selection() {
    const [selection, setSelection] = useState<RecipientSelection>({
      keyword: '',
      group: '',
      status: 1,
      all: false,
      ids: [],
    })
    return (
      <MailRecipients
        selection={selection}
        onChange={setSelection}
        limit={1000}
      />
    )
  }
  render(
    <QueryClientProvider client={client}>
      <Selection />
    </QueryClientProvider>
  )
}
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})

it('keeps recipient selection across pages and clears it explicitly', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (_url, config) => ({
    data: {
      success: true,
      data: {
        items: [
          {
            id: config?.params?.offset ? 3 : 2,
            username: config?.params?.offset ? 'bob' : 'alice',
            email: 'user@example.com',
            group: 'default',
            status: 1,
          },
        ],
        total: 11,
      },
    },
  }))
  renderRecipients()
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user bob' })
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Go to previous page' })
  )
  expect(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  ).toBeChecked()
  await userEvent.click(screen.getByRole('button', { name: 'Clear selection' }))
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice' })
  ).not.toBeChecked()
})

it('selects all matching users and leaves bulk mode when filters change', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            id: 2,
            username: 'alice',
            email: '',
            group: 'default',
            status: 1,
          },
        ],
        total: 20,
      },
    },
  })
  renderRecipients()
  await userEvent.click(
    await screen.findByRole('button', { name: 'Select all matching users' })
  )
  expect(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  ).toBeChecked()
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice' })
  ).toHaveAttribute('aria-disabled', 'true')
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Search recipients' }),
    'alice'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(get).toHaveBeenLastCalledWith(
      '/api/mail/recipients',
      expect.objectContaining({
        params: expect.objectContaining({ keyword: 'alice', offset: 0 }),
      })
    )
  )
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice' })
  ).not.toHaveAttribute('aria-disabled', 'true')
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice' })
  ).not.toBeChecked()
})

it('keeps individual checkboxes available in the mobile list', async () => {
  const original = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...original(query),
    matches:
      query.includes('max-width: 640px') ||
      query.includes('prefers-reduced-motion'),
  }))
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
  renderRecipients()
  await userEvent.click(
    await screen.findByRole('checkbox', { name: 'Select user alice' })
  )
  expect(
    screen.getByRole('checkbox', { name: 'Select user alice' })
  ).toBeChecked()
})

it('disables selecting every match when the batch limit is exceeded', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { items: [], total: 1001 } },
  })
  renderRecipients()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Select all matching users' })
    ).toBeDisabled()
  )
})
