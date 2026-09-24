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
import assert from 'node:assert/strict'
import { test } from 'node:test'

import dayjs from 'dayjs'

import { buildDailyRequestTrend } from './overview-activity'

test('daily trend adds model rows on the same day and fills missing days across a month boundary', () => {
  const start = dayjs('2026-08-31').startOf('day')
  const result = buildDailyRequestTrend(
    [
      { created_at: start.add(2, 'hour').unix(), model_name: 'a', count: 3 },
      { created_at: start.add(20, 'hour').unix(), model_name: 'b', count: 5 },
      { created_at: start.add(2, 'day').unix(), count: 2 },
    ],
    start.unix(),
    3
  )
  assert.deepEqual(result, [
    { date: '08.31', requests: 8 },
    { date: '09.01', requests: 0 },
    { date: '09.02', requests: 2 },
  ])
})

test('daily trend excludes out-of-range records and invalid counts', () => {
  const start = dayjs('2026-09-10').startOf('day')
  const result = buildDailyRequestTrend(
    [
      { created_at: start.subtract(1, 'second').unix(), count: 100 },
      { created_at: start.add(1, 'day').unix(), count: 100 },
      { created_at: start.unix(), count: -3 },
      { created_at: start.unix(), count: Number.NaN },
      { created_at: start.unix(), count: Number.POSITIVE_INFINITY },
      { created_at: start.unix() },
      { created_at: start.add(23, 'hour').unix(), count: 4 },
    ],
    start.unix(),
    1
  )
  assert.deepEqual(result, [{ date: '09.10', requests: 4 }])
})

test('daily trend keeps the selected date range for an empty response', () => {
  const start = dayjs('2026-09-10').startOf('day').unix()
  assert.deepEqual(buildDailyRequestTrend([], start, 2), [
    { date: '09.10', requests: 0 },
    { date: '09.11', requests: 0 },
  ])
})
