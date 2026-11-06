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
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { MailRecords } from '../components/mail-records'

const clients: QueryClient[] = []
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})
it('applies filters on the server and resets pagination', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            id: 1,
            email: 'alice@example.com',
            subject: 'Notice',
            kind: 'manual',
            source: 'admin',
            status: 'accepted',
            created_at: 100,
          },
        ],
        total: 21,
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <MailRecords />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: 'Notice' })
  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  await waitFor(() =>
    expect(get).toHaveBeenLastCalledWith(
      '/api/mail/records',
      expect.objectContaining({
        params: expect.objectContaining({ offset: 20 }),
      })
    )
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Search email records' }),
    'alice'
  )
  await userEvent.selectOptions(
    screen.getByRole('combobox', { name: 'Status' }),
    'failed'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(get).toHaveBeenLastCalledWith(
      '/api/mail/records',
      expect.objectContaining({
        params: expect.objectContaining({
          offset: 0,
          keyword: 'alice',
          status: 'failed',
        }),
      })
    )
  )
})
