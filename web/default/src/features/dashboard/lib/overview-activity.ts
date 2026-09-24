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
import dayjs from 'dayjs'

import type { QuotaDataItem } from '../types'

/** Group per-model usage into local calendar days, including days without traffic. */
export function buildDailyRequestTrend(
  data: QuotaDataItem[],
  startTimestamp: number,
  days: number
) {
  const start = dayjs.unix(startTimestamp).startOf('day')
  const buckets = Array.from({ length: days }, (_, index) => ({
    date: start.add(index, 'day').format('MM.DD'),
    requests: 0,
  }))
  for (const item of data) {
    const index = dayjs.unix(item.created_at).startOf('day').diff(start, 'day')
    const count = Number(item.count)
    if (
      index >= 0 &&
      index < buckets.length &&
      Number.isFinite(count) &&
      count >= 0
    ) {
      buckets[index].requests += count
    }
  }
  return buckets
}
