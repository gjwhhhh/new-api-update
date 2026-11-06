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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { MailBatches } from '../components/mail-batches'

const clients: QueryClient[] = []
function renderBatches(canOperate = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <MailBatches canOperate={canOperate} sendingEnabled />
    </QueryClientProvider>
  )
}
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})
it('disables retry for unknown outcomes and only cancels after confirmation', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            id: 1,
            subject: 'Notice',
            actor_name: 'admin',
            status: 'pending',
            total: 2,
            counts: { unknown: 1, pending: 1 },
            created_at: 100,
          },
        ],
        total: 1,
      },
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: null } })
  renderBatches()
  expect(
    await screen.findByRole('button', { name: 'Retry failed' })
  ).toBeDisabled()
  await userEvent.click(screen.getByRole('button', { name: 'Cancel pending' }))
  expect(post).not.toHaveBeenCalled()
  expect(await screen.findByRole('alertdialog')).toBeVisible()
})
it('hides retry and cancel actions when the operator only has read permission', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          {
            id: 1,
            subject: 'Notice',
            status: 'pending',
            total: 1,
            counts: { failed: 1 },
            created_at: 100,
          },
        ],
        total: 1,
      },
    },
  })
  renderBatches(false)
  await screen.findByRole('button', { name: 'Notice' })
  expect(
    screen.queryByRole('button', { name: 'Retry failed' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Cancel pending' })
  ).not.toBeInTheDocument()
})
