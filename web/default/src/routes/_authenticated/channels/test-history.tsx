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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

import { ChannelTestHistory } from '@/features/channel-test-history'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

const searchSchema = z.object({
  p: z.number().int().positive().optional().catch(1),
  channel_id: z.number().int().positive().optional().catch(undefined),
  group: z.string().trim().min(1).optional().catch(undefined),
  run_id: z.string().optional().catch(undefined),
  task_id: z.string().optional().catch(undefined),
  status: z
    .enum(['succeeded', 'failed', 'cancelled'])
    .optional()
    .catch(undefined),
  source: z
    .enum(['scheduled', 'manual_batch', 'manual_single'])
    .optional()
    .catch(undefined),
  model_name: z.string().optional().catch(undefined),
  time_range: z
    .enum(['24h', '7d', '30d', 'all', 'custom'])
    .optional()
    .catch(undefined),
  start_at: z.number().int().positive().optional().catch(undefined),
  end_at: z.number().int().positive().optional().catch(undefined),
  result_id: z.number().int().positive().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/channels/test-history')({
  beforeLoad: () => {
    const user = useAuthStore.getState().auth.user
    if (!user || user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: searchSchema,
  component: ChannelTestHistory,
})
