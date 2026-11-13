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
import dayjs from '@/lib/dayjs'

import type { CpaRatePoint } from '../types'

export type RateMetric = 'rpm' | 'tpm'

export type RatePart = 'ok' | 'fail'
export type StackDatum = {
  time: string
  part: RatePart
  value: number
  r429: number
}
export type LineDatum = { time: string; avg: number; r429: number }

/**
 * Chart rows of one range. Requests leave out 429s everywhere (they were
 * turned away, not served), the 429 count is shown in the tooltip instead.
 * 1 h: one point per minute. Wider ranges: the stack is the busiest minute
 * of each period and `avg` its per-minute average.
 */
export function buildRateRows(
  points: CpaRatePoint[],
  metric: RateMetric,
  step: number,
  format: string
) {
  const per = Math.max(1, step / 60)
  const stack: StackDatum[] = []
  const line: LineDatum[] = []
  for (const p of points) {
    const time = dayjs.unix(p.ts).format(format)
    if (metric === 'tpm') {
      const value = per > 1 ? p.peak_tokens : p.tokens
      stack.push({ time, part: 'ok', value, r429: p.r429 })
      line.push({ time, avg: p.tokens, r429: p.r429 })
      continue
    }
    const wide = per > 1
    const total = wide ? p.peak_requests : Math.max(0, p.requests - p.r429)
    const failed = Math.min(
      total,
      wide ? (p.peak_failed ?? 0) : Math.max(0, p.failed - p.r429)
    )
    stack.push({ time, part: 'ok', value: total - failed, r429: p.r429 })
    stack.push({ time, part: 'fail', value: failed, r429: 0 })
    line.push({
      time,
      avg: Math.max(0, Math.round(p.requests - p.r429 / per)),
      r429: p.r429,
    })
  }
  return { stack, line }
}
