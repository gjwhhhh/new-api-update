/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import type {
  UserChannelPriority,
  UserChannelPriorityOption,
} from '../../../types'
import { UserChannelPrioritiesDialog } from '../user-channel-priorities-dialog'

const configured: UserChannelPriority[] = [
  {
    user_id: 7,
    channel_id: 1,
    priority: 100,
    channel_name: 'Alpha',
    channel_type: 1,
    channel_status: 1,
    channel_group: 'default',
    global_priority: 10,
    weight: 20,
  },
  {
    user_id: 7,
    channel_id: 2,
    priority: 50,
    channel_name: 'Beta',
    channel_type: 1,
    channel_status: 1,
    channel_group: 'default',
    global_priority: 5,
    weight: 10,
  },
]

const options: UserChannelPriorityOption[] = [
  ...configured.map((item) => ({
    channel_id: item.channel_id,
    channel_name: item.channel_name,
    channel_type: item.channel_type,
    channel_status: item.channel_status,
    channel_group: item.channel_group,
    global_priority: item.global_priority,
    weight: item.weight,
  })),
  {
    channel_id: 3,
    channel_name: 'Gamma',
    channel_type: 1,
    channel_status: 1,
    channel_group: 'premium',
    global_priority: 30,
    weight: 8,
  },
  {
    channel_id: 4,
    channel_name: 'Delta',
    channel_type: 1,
    channel_status: 1,
    channel_group: 'premium',
    global_priority: 20,
    weight: 6,
  },
]

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UserChannelPrioritiesDialog
        open
        onOpenChange={() => undefined}
        user={{ id: 7, username: 'route-user' }}
      />
    </QueryClientProvider>
  )
}

function mockSuccessfulQueries(total = options.length) {
  return vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/7/channel-priorities') {
      return { data: { success: true, data: { items: configured } } }
    }
    if (url === '/api/user/7/channel-priorities/options') {
      return {
        data: {
          success: true,
          data: { items: options, total, page: 1, page_size: 20 },
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('UserChannelPrioritiesDialog editing', () => {
  it('saves edits, restored inheritance, and multiple added channels as a differential patch', async () => {
    mockSuccessfulQueries()
    const patch = vi.spyOn(api, 'patch').mockResolvedValue({
      data: { success: true, data: { items: [] } },
    })
    renderDialog()

    const alpha = await screen.findByRole('textbox', {
      name: 'User priority Alpha',
    })
    expect(
      screen.getByRole('textbox', { name: 'User priority Beta' })
    ).toHaveValue('50')

    await userEvent.clear(alpha)
    await userEvent.type(alpha, '120')
    await userEvent.click(
      screen.getByRole('button', {
        name: 'Restore inherited priority for Beta',
      })
    )
    await userEvent.click(screen.getByRole('button', { name: 'Add Gamma' }))
    await userEvent.click(screen.getByRole('button', { name: 'Add Delta' }))
    expect(
      screen.getByRole('textbox', { name: 'User priority Gamma' })
    ).toHaveValue('30')
    expect(
      screen.getByRole('textbox', { name: 'User priority Delta' })
    ).toHaveValue('20')

    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(patch).toHaveBeenCalledWith('/api/user/7/channel-priorities', {
        upserts: [
          { channel_id: 1, priority: 120 },
          { channel_id: 3, priority: 30 },
          { channel_id: 4, priority: 20 },
        ],
        deletes: [2],
      })
    )
  })

  it('marks non-integer and out-of-range values invalid and prevents saving', async () => {
    mockSuccessfulQueries()
    const patch = vi.spyOn(api, 'patch')
    renderDialog()

    const alpha = await screen.findByRole('textbox', {
      name: 'User priority Alpha',
    })
    const save = screen.getByRole('button', { name: 'Save changes' })
    await userEvent.clear(alpha)
    await userEvent.type(alpha, '1.5')
    expect(alpha).toHaveAttribute('aria-invalid', 'true')
    const integerError = screen.getByText('Priority must be an integer')
    expect(integerError).toBeVisible()
    expect(integerError).toHaveAttribute('role', 'alert')
    expect(alpha).toHaveAttribute('aria-describedby', integerError.id)
    expect(save).toBeDisabled()

    await userEvent.clear(alpha)
    await userEvent.type(alpha, '1000000001')
    expect(
      screen.getByText(
        'Priority must be between -1,000,000,000 and 1,000,000,000'
      )
    ).toBeVisible()
    expect(save).toBeDisabled()
    expect(patch).not.toHaveBeenCalled()

    await userEvent.clear(alpha)
    await userEvent.type(alpha, '100')
    expect(alpha).not.toHaveAttribute('aria-invalid', 'true')
    expect(alpha).not.toHaveAttribute('aria-describedby')
    expect(integerError).not.toBeInTheDocument()
  })
})

describe('UserChannelPrioritiesDialog loading and discovery', () => {
  it('treats a null configured-priority list as empty instead of crashing the route', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/user/7/channel-priorities') {
        return { data: { success: true, data: { items: null } } }
      }
      return {
        data: {
          success: true,
          data: {
            items: options,
            total: options.length,
            page: 1,
            page_size: 20,
          },
        },
      }
    })
    renderDialog()

    expect(
      await screen.findByText('No channel priority overrides')
    ).toBeVisible()
  })

  it('supports paginated channel discovery and resets to page one after searching', async () => {
    const get = mockSuccessfulQueries(45)
    renderDialog()
    await screen.findByText('Page 1 of 3')

    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith(
        '/api/user/7/channel-priorities/options',
        expect.objectContaining({
          params: expect.objectContaining({ p: 2 }),
        })
      )
    )
    await userEvent.type(screen.getByLabelText('Search channels'), 'gamma')
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith(
        '/api/user/7/channel-priorities/options',
        expect.objectContaining({
          params: expect.objectContaining({ keyword: 'gamma', p: 1 }),
        })
      )
    )
  })

  it('shows a retryable error when configured priorities cannot be loaded', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/user/7/channel-priorities') {
        throw new Error('load failed')
      }
      return {
        data: {
          success: true,
          data: {
            items: options,
            total: options.length,
            page: 1,
            page_size: 20,
          },
        },
      }
    })
    renderDialog()

    expect(
      await screen.findByText('Failed to load channel priorities')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled()
  })
})
