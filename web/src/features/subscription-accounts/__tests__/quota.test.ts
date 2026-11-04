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

import { cardUsageOf, lastRequestAt, socksStateOf } from '../lib/card'
import {
  buildDollarWindows,
  buildQuotaWindows,
  buildResetGrants,
  quotaLevel,
  remainingPercent,
  sortAccounts,
} from '../lib/quota'
import type { CpaAccount } from '../types'

function account(name: string, left?: number): CpaAccount {
  return {
    instance_id: 1,
    name,
    is_claude: true,
    updated_at: 0,
    reset_grants:
      left === undefined
        ? undefined
        : {
            eligible: true,
            grants: [{ id: 'g', resets_total: 3, resets_left: left }],
          },
  }
}

describe('subscription accounts quota helpers', () => {
  it('uses CPA panel tiers on remaining: >=70 green, >=30 amber, <30 red', () => {
    expect(quotaLevel(remainingPercent(30))).toBe('ok') // 70 left
    expect(quotaLevel(remainingPercent(31))).toBe('warn') // 69 left
    expect(quotaLevel(remainingPercent(70))).toBe('warn') // 30 left
    expect(quotaLevel(remainingPercent(71))).toBe('danger') // 29 left
    expect(quotaLevel(remainingPercent(null))).toBe('unknown')
  })

  it('puts accounts with reset grants left first', () => {
    const sorted = sortAccounts([
      account('a'),
      account('b', 0),
      account('c', 1),
      account('d', 2),
    ])
    expect(sorted.map((item) => item.name)).toEqual(['d', 'c', 'a', 'b'])
  })

  it('prefers the weekly_scoped fable limit over iguana_necktie', () => {
    const windows = buildQuotaWindows({
      five_hour: { utilization: 10, resets_at: '2026-11-04T00:00:00Z' },
      iguana_necktie: { utilization: 99, resets_at: null },
      limits: [
        {
          kind: 'weekly_scoped',
          percent: 20,
          is_active: true,
          scope: { model: { display_name: 'Fable' } },
        },
      ],
    })
    expect(windows.map((w) => [w.id, w.usedPercent])).toEqual([
      ['five-hour', 10],
      ['seven-day-fable', 20],
    ])
  })

  it('collects dollar balances and sums resets left', () => {
    expect(
      buildDollarWindows({
        iguana_necktie: {
          limit_dollars: 50,
          used_dollars: 10,
          remaining_dollars: 40,
          utilization: 20,
          resets_at: null,
        },
      })
    ).toHaveLength(1)
    expect(
      buildResetGrants({
        eligible: true,
        grants: [
          { id: 'a', resets_total: 2, resets_left: 1 },
          { id: 'b', resets_total: 2, resets_left: 2 },
        ],
      })?.resetsLeft
    ).toBe(3)
  })
})

describe('subscription accounts card helpers', () => {
  const base = account('a')

  it('maps proxy state to green / red / grey', () => {
    expect(socksStateOf({ ...base, proxy_url: 'socks5h://u:****@h:1' })).toBe(
      'ok'
    )
    expect(
      socksStateOf({
        ...base,
        proxy_error:
          'blocked: account has no valid socks5/socks5h proxy_url, request not sent',
      })
    ).toBe('missing')
    expect(socksStateOf(base)).toBe('missing')
    expect(
      socksStateOf({
        ...base,
        proxy_url: 'socks5h://h:1',
        proxy_fail_streak: 1,
      })
    ).toBe('failed')
    expect(
      socksStateOf({ ...base, proxy_url: 'socks5h://h:1', auto_disabled_at: 5 })
    ).toBe('failed')
  })

  it('looks up card usage by instance and auth_index', () => {
    const usage = { '1/abc': { requests: 2, tokens: 3, cost_usd: 1 } }
    expect(
      cardUsageOf(usage, { ...base, credential: { auth_index: 'abc' } })
    ).toEqual(usage['1/abc'])
    expect(cardUsageOf(usage, base)).toBeNull()
  })

  it('prefers queue time and falls back to recent_requests buckets', () => {
    expect(lastRequestAt({ ...base, last_request_at: 123 })).toBe(123)
    const recent = Array.from({ length: 20 }, () => ({ success: 0, failed: 0 }))
    recent[17] = { success: 1, failed: 0 }
    expect(
      lastRequestAt({
        ...base,
        credential_synced_at: 10_000,
        credential: { recent_requests: recent },
      })
    ).toBe(10_000 - 2 * 600)
    expect(
      lastRequestAt({ ...base, credential: { recent_requests: recent } })
    ).toBe(0)
  })
})
