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
  buildEffectiveQuota,
  buildPassiveQuota,
  nextAutoRefreshAt,
} from '../lib/quota'
import type { CpaAccount } from '../types'

const NOW_MS = Date.parse('2026-11-06T04:00:00Z')
const NOW_SEC = NOW_MS / 1000

function signals(extra: Record<string, string> = {}) {
  return {
    'Anthropic-Ratelimit-Unified-5h-Utilization': '0.28',
    'Anthropic-Ratelimit-Unified-5h-Reset': String(NOW_SEC + 7200),
    'Anthropic-Ratelimit-Unified-5h-Status': 'allowed',
    'Anthropic-Ratelimit-Unified-7d-Utilization': '0.53',
    'Anthropic-Ratelimit-Unified-7d-Reset': String(NOW_SEC + 3 * 86400),
    'Anthropic-Ratelimit-Unified-7d-Status': 'allowed',
    ...extra,
  }
}

function account(over: Partial<CpaAccount> = {}): CpaAccount {
  return { instance_id: 1, name: 'a.json', is_claude: true, updated_at: 0, ...over }
}

describe('passive quota from chats', () => {
  it('parses 5h / 7d windows from CPA quota signals', () => {
    const quota = buildPassiveQuota({
      quota: {
        observed_at: new Date(NOW_MS - 12_000).toISOString(),
        signals: signals({ 'Anthropic-Ratelimit-Unified-5h-Status': 'rejected' }),
      },
    })
    expect(quota?.observedAt).toBe(NOW_SEC - 12)
    expect(quota?.windows.map((w) => [w.id, Math.round(w.usedPercent ?? -1), w.limited])).toEqual([
      ['five-hour', 28, true],
      ['seven-day', 53, false],
    ])
    expect(Date.parse(quota?.windows[0].resetsAt ?? '')).toBe(NOW_MS + 7_200_000)
  })

  it('returns null without signals or observed_at', () => {
    expect(buildPassiveQuota(undefined)).toBeNull()
    expect(buildPassiveQuota({ quota: { signals: signals() } })).toBeNull()
    expect(
      buildPassiveQuota({ quota: { observed_at: '2026-11-06T00:00:00Z', signals: {} } })
    ).toBeNull()
  })

  it('uses the newer source and keeps other windows from the refresh', () => {
    const usage = {
      five_hour: { utilization: 90, resets_at: new Date(NOW_MS + 3_600_000).toISOString() },
      seven_day: { utilization: 90, resets_at: new Date(NOW_MS + 86_400_000).toISOString() },
      seven_day_opus: { utilization: 40, resets_at: new Date(NOW_MS + 86_400_000).toISOString() },
    }
    const credential = {
      quota: { observed_at: new Date(NOW_MS - 5_000).toISOString(), signals: signals() },
    }
    const chatNewer = buildEffectiveQuota(
      account({ usage, credential, quota_observed_at: NOW_SEC - 3600 }),
      NOW_MS
    )
    expect(chatNewer.source).toBe('chat')
    expect(chatNewer.observedAt).toBe(NOW_SEC - 5)
    expect(chatNewer.windows.map((w) => [w.id, Math.round(w.usedPercent ?? -1)])).toEqual([
      ['five-hour', 28],
      ['seven-day', 53],
      ['seven-day-opus', 40],
    ])

    const refreshNewer = buildEffectiveQuota(
      account({ usage, credential, quota_observed_at: NOW_SEC }),
      NOW_MS
    )
    expect(refreshNewer.source).toBe('refresh')
    expect(refreshNewer.windows[0].usedPercent).toBe(90)
  })

  it('shows a window whose reset time passed as full', () => {
    const credential = {
      quota: {
        observed_at: new Date(NOW_MS - 9 * 3_600_000).toISOString(),
        signals: signals({
          'Anthropic-Ratelimit-Unified-5h-Reset': String(NOW_SEC - 60),
          'Anthropic-Ratelimit-Unified-5h-Status': 'rejected',
        }),
      },
    }
    const quota = buildEffectiveQuota(account({ credential }), NOW_MS)
    expect(quota.windows[0]).toMatchObject({ usedPercent: 0, resetDone: true, limited: false })
    expect(quota.windows[1].resetDone).toBeUndefined()
  })

  it('reports no source without any data', () => {
    expect(buildEffectiveQuota(account(), NOW_MS)).toEqual({
      windows: [],
      source: null,
      observedAt: 0,
    })
  })
})

describe('daily random refresh', () => {
  it('returns the next planned slot or null when done', () => {
    expect(nextAutoRefreshAt(account())).toBeNull()
    expect(nextAutoRefreshAt(account({ auto_refresh_plan: [10, 20], auto_refresh_done: 1 }))).toBe(20)
    expect(nextAutoRefreshAt(account({ auto_refresh_plan: [10, 20], auto_refresh_done: 2 }))).toBeNull()
  })
})
