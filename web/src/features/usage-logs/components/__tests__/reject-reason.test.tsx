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
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const queryClients: QueryClient[] = []

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 5,
    content: 'request rejected',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function renderDetails(isAdmin: boolean, logType = 5): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(['status'], {}, { updatedAt: freshAt })
  queryClients.push(queryClient)

  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={{
          ...makeLog({
            admin_info: { reject_reason: 'blocked by channel policy' },
          }),
          type: logType,
        }}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.restoreAllMocks()
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
})

describe('usage log reject reason', () => {
  test('shows the nested admin reject reason to admins', () => {
    renderDetails(true)

    expect(screen.getByText('Reject Reason')).toBeInTheDocument()
    expect(screen.getByText('blocked by channel policy')).toBeInTheDocument()
  })

  test('hides the nested admin reject reason from non-admin users', () => {
    const get = vi.spyOn(api, 'get')
    renderDetails(false)

    expect(screen.queryByText('Reject Reason')).toBeNull()
    expect(screen.queryByText('blocked by channel policy')).toBeNull()
    expect(
      screen.queryByRole('tab', { name: 'Conversation content' })
    ).toBeNull()
    expect(get).not.toHaveBeenCalled()
  })
})

describe('conversation content in usage log details', () => {
  test('loads only when opened and shows the matching continuation as plain text', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          audit: { request_id: 'root-1', status: 'completed' },
          request: JSON.stringify({
            schema_version: 2,
            user_input: 'A question',
          }),
          response: JSON.stringify({
            schema_version: 2,
            assistant_text: 'Root reply',
          }),
          response_segments: [
            JSON.stringify({
              schema_version: 2,
              assistant_text: '<b>Continuation reply</b>',
            }),
          ],
          matched_segment: { response_index: 0, truncated: true },
        },
      },
    })
    renderDetails(true, 2)
    expect(get).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('tab', { name: 'Conversation content' }))
    expect(await screen.findByText('A question')).toBeVisible()
    expect(
      screen.getByText('<b>Continuation reply</b>', { exact: true })
    ).toBeVisible()
    expect(screen.getByText('Response truncated')).toBeVisible()
    expect(screen.getByText(/Root reply/)).not.toBeVisible()
    fireEvent.click(screen.getByText('Combined turn response'))
    expect(screen.getByText(/Root reply/)).toBeVisible()
    expect(
      screen.getByRole('link', { name: 'View conversation audit' })
    ).toHaveAttribute('href', '/conversation-audit/?request_id=req-1')
    expect(get).toHaveBeenCalledWith(
      '/api/custom/conversation-audits/req-1',
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    fireEvent.click(screen.getByRole('tab', { name: 'Details' }))
    expect(screen.queryByText('A question')).toBeNull()
  })

  test.each([
    [404, 'No conversation content available'],
    [403, 'Access denied'],
    [503, 'Could not load conversation content'],
  ])(
    'shows an inline state for a failed log when the audit API returns %i',
    async (status, message) => {
      vi.spyOn(api, 'get').mockRejectedValueOnce({ response: { status } })
      renderDetails(true)
      fireEvent.click(screen.getByRole('tab', { name: 'Conversation content' }))
      expect(await screen.findByText(message)).toBeVisible()
    }
  )
})
