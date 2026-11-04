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

import {
  accountDisplayName,
  buildCostSummary,
  buildPoolHealth,
  estimateExhaustSec,
  failRate,
  tightestAccounts,
} from '../lib/pool'
import type { CpaAccount, CpaQuotaPoint, CpaStatsReport } from '../types'

const NOW = 1_800_000_000

function acc(name: string, extra: Partial<CpaAccount> = {}): CpaAccount {
  return { instance_id: 1, name, is_claude: true, updated_at: 0, ...extra }
}

describe('pool health', () => {
  it('splits accounts into available / disabled / cooling and counts SOCKS', () => {
    const health = buildPoolHealth(
      [
        acc('a', { proxy_url: 'socks5://x' }),
        acc('b', { credential: { disabled: true } }),
        acc('c', {
          credential: { unavailable: true },
          proxy_url: 'socks5://y',
        }),
        acc('d', {
          credential: {
            next_retry_after: new Date((NOW + 60) * 1000).toISOString(),
          },
          proxy_fail_streak: 1,
          proxy_url: 'socks5://z',
        }),
        acc('e', {
          credential: {
            next_retry_after: new Date((NOW - 60) * 1000).toISOString(),
            success: 5,
            failed: 2,
          },
        }),
        acc('codex', { is_claude: false }),
      ],
      NOW
    )
    expect(health.total).toBe(5)
    expect(health.available).toBe(2)
    expect(health.disabled).toBe(1)
    expect(health.cooling).toBe(2)
    expect(health.socks).toEqual({ ok: 2, failed: 1, missing: 2 })
    expect(health.cpaSuccess).toBe(5)
    expect(health.cpaFailed).toBe(2)
  })

  it('sums reset grants and reports the earliest end date', () => {
    const health = buildPoolHealth(
      [
        acc('a', {
          reset_grants: {
            eligible: true,
            grants: [
              {
                id: 'g1',
                resets_total: 3,
                resets_left: 2,
                ends_at: '2026-12-01T00:00:00Z',
              },
              {
                id: 'g2',
                resets_total: 1,
                resets_left: 0,
                ends_at: '2026-11-10T00:00:00Z',
              },
            ],
          },
        }),
        acc('b', {
          reset_grants: {
            eligible: true,
            grants: [
              {
                id: 'g3',
                resets_total: 1,
                resets_left: 1,
                ends_at: '2026-12-01T08:00:00Z',
              },
            ],
          },
        }),
      ],
      NOW
    )
    expect(health.resetsLeft).toBe(3)
    expect(health.earliestEnds).toBe('2026-12-01')
    expect(health.earliestEndsCount).toBe(3)
  })
})

describe('quota trend', () => {
  const pt = (ts: number, used: number): CpaQuotaPoint => ({
    instance_id: 1,
    name: 'a',
    ts,
    five_hour: used,
  })

  it('extrapolates the last hour linearly', () => {
    // 10% used per 30 min, 60% left → 3 hours
    const sec = estimateExhaustSec(
      [pt(NOW - 1800, 30), pt(NOW, 40)],
      'five_hour',
      NOW
    )
    expect(sec).toBe(6 * 1800)
  })

  it('returns null without an upward trend or enough recent points', () => {
    expect(estimateExhaustSec([pt(NOW, 40)], 'five_hour', NOW)).toBeNull()
    expect(
      estimateExhaustSec([pt(NOW - 600, 40), pt(NOW, 40)], 'five_hour', NOW)
    ).toBeNull()
    expect(
      estimateExhaustSec([pt(NOW - 7200, 10), pt(NOW, 40)], 'five_hour', NOW)
    ).toBeNull()
  })

  it('lists the tightest enabled accounts and drops estimates past the reset', () => {
    const usage = (used: number, resetsInSec: number) => ({
      five_hour: {
        utilization: used,
        resets_at: new Date((NOW + resetsInSec) * 1000).toISOString(),
      },
    })
    const list = tightestAccounts(
      [
        acc('a', {
          usage: usage(40, 99999),
          credential: { email: 'alice@x.com' },
        }),
        acc('b', { usage: usage(90, 600) }),
        acc('c', { usage: usage(70, 99999) }),
        acc('d', { usage: usage(99, 600), credential: { disabled: true } }),
        acc('e', { usage: usage(10, 99999) }),
      ],
      'five-hour',
      [
        { instance_id: 1, name: 'a', ts: NOW - 1800, five_hour: 30 },
        { instance_id: 1, name: 'a', ts: NOW, five_hour: 40 },
        { instance_id: 1, name: 'b', ts: NOW - 1800, five_hour: 80 },
        { instance_id: 1, name: 'b', ts: NOW, five_hour: 90 },
      ],
      NOW
    )
    expect(list.map((i) => i.name)).toEqual(['b', 'c', 'alice'])
    // b would run out in 30 min but resets in 10 min → no estimate
    expect(list[0].exhaustInSec).toBeNull()
    expect(list[2].exhaustInSec).toBe(6 * 1800)
  })

  it('uses the email prefix as display name', () => {
    expect(
      accountDisplayName({ name: 'f.json', credential: { email: 'x@y.z' } })
    ).toBe('x')
    expect(accountDisplayName({ name: 'f.json' })).toBe('f.json')
  })
})

describe('cost summary', () => {
  it('computes payback from revenue over plan + proxy cost', () => {
    const base = {
      instance_id: 1,
      auth_index: '',
      disabled: false,
      requests: 0,
      failed: 0,
      tokens: 0,
      input_tokens: 0,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      avg_latency_ms: 0,
      unpriced_requests: 0,
      revenue_usd: 0,
    }
    const report: CpaStatsReport = {
      from: 0,
      to: 0,
      days: 30,
      estimated: true,
      daily: [],
      quota_points: [],
      accounts: [
        {
          ...base,
          name: 'a',
          cost_usd: 300,
          plan_cost_usd: 100,
          plan_cost_known: true,
          proxy_cost_usd: 5,
        },
        {
          ...base,
          name: 'b',
          cost_usd: 50,
          plan_cost_usd: 0,
          plan_cost_known: false,
          proxy_cost_usd: 5,
        },
      ],
      instances: [
        { instance_id: 1, channel_ids: [], revenue_usd: 220, requests: 9 },
      ],
    }
    const sum = buildCostSummary(report)
    expect(sum.planCostUSD).toBe(100)
    expect(sum.unknownPlanAccounts).toBe(1)
    expect(sum.proxyCostUSD).toBe(10)
    expect(sum.officialCostUSD).toBe(350)
    expect(sum.revenueUSD).toBe(220)
    expect(sum.payback).toBe(2)
    expect(buildCostSummary(undefined).payback).toBeNull()
  })

  it('fail rate is null without requests', () => {
    expect(failRate(null)).toBeNull()
    expect(
      failRate({
        day: 0,
        requests: 4,
        failed: 1,
        tokens: 0,
        cost_usd: 0,
        revenue_usd: 0,
      })
    ).toBe(25)
  })
})
