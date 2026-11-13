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
import { describe, expect, it } from 'vitest'

import { buildRateRows } from '../lib/rate'
import type { CpaRatePoint } from '../types'

const point = (over: Partial<CpaRatePoint>): CpaRatePoint => ({
  ts: 1_794_410_400,
  requests: 0,
  tokens: 0,
  failed: 0,
  r429: 0,
  peak_requests: 0,
  peak_tokens: 0,
  ...over,
})

describe('rate chart rows', () => {
  it('splits one minute into succeeded and failed, without 429s', () => {
    const rows = buildRateRows(
      [point({ requests: 12, failed: 5, r429: 2 })],
      'rpm',
      60,
      'HH:mm'
    )
    expect(rows.stack.map((d) => [d.part, d.value])).toEqual([
      ['ok', 7],
      ['fail', 3],
    ])
    // The 429 count rides on the first row only, so the tooltip lists it once.
    expect(rows.stack.map((d) => d.r429)).toEqual([2, 0])
    expect(rows.line[0].avg).toBe(10)
  })

  it('uses the busiest minute for bars and the average for the line', () => {
    const rows = buildRateRows(
      [
        point({
          requests: 30,
          failed: 40,
          r429: 20,
          peak_requests: 50,
          peak_failed: 4,
        }),
      ],
      'rpm',
      600,
      'MM-DD HH:mm'
    )
    expect(rows.stack.map((d) => [d.part, d.value])).toEqual([
      ['ok', 46],
      ['fail', 4],
    ])
    // 30 a minute on average, minus 20 429s spread over 10 minutes.
    expect(rows.line[0].avg).toBe(28)
  })

  it('never shows more failures than requests', () => {
    const rows = buildRateRows(
      [point({ peak_requests: 3, peak_failed: 9 })],
      'rpm',
      1800,
      'MM-DD HH:mm'
    )
    expect(rows.stack.map((d) => d.value)).toEqual([0, 3])
  })

  it('keeps tokens in one part', () => {
    const minute = buildRateRows(
      [point({ tokens: 900, peak_tokens: 1500 })],
      'tpm',
      60,
      'HH:mm'
    )
    expect(minute.stack).toHaveLength(1)
    expect(minute.stack[0].value).toBe(900)
    const wide = buildRateRows(
      [point({ tokens: 900, peak_tokens: 1500 })],
      'tpm',
      7200,
      'MM-DD HH:mm'
    )
    expect(wide.stack[0].value).toBe(1500)
    expect(wide.line[0].avg).toBe(900)
  })
})
