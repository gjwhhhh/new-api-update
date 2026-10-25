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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ChannelStatusPage } from '../index'
import type { ChannelStatusItem, GroupStatusItem } from '../types'

const group: GroupStatusItem = {
  group: 'vip',
  name: 'vip',
  description: '',
  ratio: 1,
  request_count: 10,
  success_count: 10,
  success_rate: 1,
  avg_ttft_ms: 10,
  avg_latency_ms: 100,
  avg_tps: 20,
  series: [],
  models: [],
}
const channel: ChannelStatusItem = {
  channel_id: 3,
  channel_name: 'production',
  channel_type: 1,
  channel_status: 1,
  groups: ['vip'],
  health: 'running',
  request_count: 10,
  success_count: 10,
  success_rate: 1,
  avg_ttft_ms: 10,
  avg_latency_ms: 100,
  avg_tps: 20,
  series: [],
}
let client: QueryClient
let hitRate = 0.25

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  hitRate = 0.25
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
  })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    let data: unknown
    if (url === '/api/perf-metrics/groups') {
      data = {
        groups: [group],
        bucket_seconds: 3600,
        start_ts: 0,
        end_ts: 0,
        upstream_cache_visible_to_users: true,
      }
    } else if (url === '/api/channel/status-metrics') {
      data = {
        items: [channel],
        page: 1,
        page_size: 24,
        total: 1,
        provider_types: [1],
        health_counts: { running: 1, fluctuating: 0, abnormal: 0, no_data: 0 },
      }
    } else if (url.includes('upstream-cache-stats')) {
      data = [
        {
          scope: url.startsWith('/api/channel/') ? 'channel' : 'group',
          group: 'vip',
          channel_id: 3,
          windows: {
            '1h': {
              total: 4,
              hit: hitRate * 4,
              request_hit_rate: hitRate,
              token_hit_rate: hitRate,
              cached_tokens: hitRate * 100,
              prompt_tokens: 100,
            },
            '24h': {
              total: 4,
              hit: hitRate * 4,
              request_hit_rate: hitRate,
              token_hit_rate: hitRate,
              cached_tokens: hitRate * 100,
              prompt_tokens: 100,
            },
          },
        },
      ]
    } else if (url === '/api/group/') {
      data = ['vip']
    } else {
      throw new Error(`Unexpected GET ${url}`)
    }
    return { data: { success: true, data } }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.reset()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

async function renderStatus(view: 'groups' | 'channels') {
  const root = createRootRoute()
  const route = createRoute({
    getParentRoute: () => root,
    path: '/status',
    component: ChannelStatusPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory({ initialEntries: ['/status'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findAllByText('25.00%')
  if (view === 'channels') {
    fireEvent.click(screen.getByRole('tab', { name: 'Channels' }))
    await screen.findByText('production')
    await screen.findAllByText('25.00%')
  }
  await waitFor(() => expect(client.isFetching()).toBe(0))
}

describe.each(['groups', 'channels'] as const)(
  '%s cache hit freshness',
  (view) => {
    it('updates the displayed cache hit rate when Refresh is clicked with the same list', async () => {
      await renderStatus(view)
      hitRate = 0.75
      fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
      await screen.findAllByText('75.00%')
      expect(screen.queryByText('25.00%')).not.toBeInTheDocument()
    })

    it('updates the displayed cache hit rate on the next minute with the same list', async () => {
      vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
      await renderStatus(view)
      hitRate = 0.75
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000)
      })
      await screen.findAllByText('75.00%')
      expect(screen.queryByText('25.00%')).not.toBeInTheDocument()
    })
  }
)
