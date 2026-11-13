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

import { failCategoryOf, groupFailReasons, sevenDayCounts } from '../lib/card'
import {
  cycleBasisKey,
  cycleBreakAfter,
  cycleOverview,
  cycleUsedText,
} from '../lib/cycle'
import { buildResetGrants } from '../lib/quota'
import type { CpaCycleAccount, CpaCycleRow } from '../types'

function cycle(over: Partial<CpaCycleRow>): CpaCycleRow {
  return {
    id: 1,
    start_ts: 0,
    end_ts: 604800,
    open: false,
    end_reason: 'natural',
    plan_type: 'max_20x',
    tokens: 0,
    cache_read_tokens: 0,
    used_percent: null,
    percent_at: 0,
    percent_source: 'refresh',
    hit_limit: false,
    estimated_full: null,
    change_percent: null,
    backfilled: false,
    ...over,
  }
}

describe('failure reasons (design 2026-11-11, option C)', () => {
  it('maps status codes to the five categories', () => {
    expect(failCategoryOf(429)).toBe('rate')
    expect(failCategoryOf(502)).toBe('proxy')
    expect(failCategoryOf(0)).toBe('proxy')
    expect(failCategoryOf(529)).toBe('upstream')
    expect(failCategoryOf(500)).toBe('upstream')
    expect(failCategoryOf(401)).toBe('auth')
    expect(failCategoryOf(403)).toBe('auth')
    expect(failCategoryOf(400)).toBe('other')
  })

  it('groups reasons and keeps empty categories at 0', () => {
    expect(
      groupFailReasons([
        { status_code: 429, count: 28 },
        { status_code: 502, count: 7 },
        { status_code: 529, count: 3 },
        { status_code: 503, count: 1 },
        { status_code: 400, count: 2 },
      ])
    ).toEqual({ rate: 28, proxy: 7, upstream: 4, auth: 0, other: 2 })
    expect(groupFailReasons(undefined).rate).toBe(0)
  })

  it('derives 7-day success / failed, null without collected data', () => {
    expect(
      sevenDayCounts({
        requests: 0,
        tokens: 0,
        cost_usd: 0,
        requests_7d: 1325,
        failed_7d: 41,
      })
    ).toEqual({ success: 1284, failed: 41 })
    expect(sevenDayCounts({ requests: 5, tokens: 0, cost_usd: 0 })).toBeNull()
    expect(sevenDayCounts(null)).toBeNull()
  })
})

describe('reset grant flags', () => {
  it('reads usable_now / paused, null when missing', () => {
    const grants = buildResetGrants({
      eligible: true,
      grants: [
        { id: 'a', resets_total: 1, resets_left: 1, usable_now: true },
        { id: 'b', resets_total: 1, resets_left: 1, paused: true },
        { id: 'c', resets_total: 1, resets_left: 1 },
      ],
    })?.grants
    expect(grants?.map((g) => [g.usableNow, g.paused])).toEqual([
      [true, null],
      [null, true],
      [null, null],
    ])
  })
})

describe('cycle comparison helpers', () => {
  it('counts a 429 as 100% and the most accurate basis', () => {
    const limited = cycle({ hit_limit: true, used_percent: 97 })
    expect(cycleUsedText(limited)).toBe('100%')
    expect(cycleBasisKey(limited)).toBe('429 used up, most accurate')
    expect(cycleUsedText(cycle({ used_percent: 61.6 }))).toBe('62%')
    expect(cycleUsedText(cycle({}))).toBe('-')
    expect(cycleBasisKey(cycle({}))).toBeNull()
    expect(
      cycleBasisKey(cycle({ used_percent: 40, percent_source: 'chat' }))
    ).toBe('Chat')
  })

  it('draws a divider on plan changes only', () => {
    const rows = [
      cycle({ id: 3, plan_type: 'max_20x' }),
      cycle({ id: 2, plan_type: 'max_20x', end_reason: 'manual_reset' }),
      cycle({ id: 1, plan_type: 'max_5x', end_reason: 'plan_change' }),
      cycle({ id: 0, plan_type: 'max_5x' }),
    ]
    expect(cycleBreakAfter(rows, 0)).toBe(false)
    expect(cycleBreakAfter(rows, 1)).toBe(true)
    expect(cycleBreakAfter(rows, 2)).toBe(false)
    expect(cycleBreakAfter(rows, 3)).toBe(false)
  })
})

describe('cycle overview (one row per account)', () => {
  const account = (name: string, cycles: CpaCycleRow[]): CpaCycleAccount => ({
    instance_id: 1,
    name,
    windows: [{ window: 'seven_day', cycles }],
  })

  it('puts the biggest drop first and accounts without comparison last', () => {
    const rows = cycleOverview(
      [
        account('steady', [cycle({ estimated_full: 100, change_percent: 2 })]),
        account('new', [cycle({ open: true })]),
        account('dropped', [
          cycle({ estimated_full: 80, change_percent: -20 }),
        ]),
        account('other-window', []),
      ],
      'seven_day'
    )
    expect(rows.map((r) => r.account.name)).toEqual([
      'dropped',
      'steady',
      'new',
    ])
    expect(rows.map((r) => r.dropped)).toEqual([true, false, false])
    expect(rows[2].change).toBeNull()
  })

  it('uses the newest period that has an estimate', () => {
    const [row] = cycleOverview(
      [
        account('a', [
          cycle({ id: 3, open: true, used_percent: 4 }),
          cycle({ id: 2, estimated_full: 90, change_percent: -10 }),
          cycle({ id: 1, estimated_full: 100 }),
        ]),
      ],
      'seven_day'
    )
    expect(row.latest?.id).toBe(2)
    expect(row.change).toBe(-10)
    expect(row.dropped).toBe(true)
  })

  it('skips accounts that have no period in the window', () => {
    expect(
      cycleOverview([account('a', [cycle({})])], 'seven_day_opus')
    ).toEqual([])
  })
})
