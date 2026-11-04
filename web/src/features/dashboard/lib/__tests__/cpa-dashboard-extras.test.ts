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
import { describe, expect, test } from 'vitest'

import type { FlowQuotaDataItem } from '../../types'
import { buildQueryParams } from '../filters'
import { buildDashboardFlowData } from '../flow'
import { errorRateFromSuccessRate } from '../stats'

const base = {
  user_id: 1,
  username: 'alice',
  node_name: 'node-a',
  token_id: 11,
  token_name: 'primary',
  use_group: 'vip',
  model_name: 'claude-x',
}

const rows: FlowQuotaDataItem[] = [
  { ...base, channel_id: 5, cpa_account: 'a@x', count: 3, quota: 60 },
  { ...base, channel_id: 5, cpa_account: 'b@x', count: 1, quota: 40 },
  { ...base, channel_id: 5, cpa_account: '__unknown__', count: 1, quota: 5 },
  { ...base, channel_id: 9, count: 2, quota: 20 },
]

describe('CPA account flow layer', () => {
  test('root flow adds the CPA account column only for CPA channels', () => {
    const result = buildDashboardFlowData(rows, 'quota', {
      role: 'root',
      unknownCpaAccountLabel: 'Unknown',
    })
    const toAccount = result.flow.links
      .filter((link) => link.target.startsWith('cpa_account:'))
      .map((link) => [link.source, link.target, link.value])
    expect(toAccount).toEqual([
      ['channel:5', 'cpa_account:__unknown__', 5],
      ['channel:5', 'cpa_account:a@x', 60],
      ['channel:5', 'cpa_account:b@x', 40],
    ])
    expect(result.flow.links.some((link) => link.source === 'channel:9')).toBe(
      false
    )
    expect(
      result.flow.nodes.find((node) => node.id === 'cpa_account:__unknown__')
        ?.label
    ).toBe('Unknown')
    // Splitting must not change totals.
    expect(result.summary.quota).toBe(125)
  })

  test('admin flow never shows the CPA account column', () => {
    const result = buildDashboardFlowData(rows, 'quota', { role: 'admin' })
    expect(result.flow.nodes.some((node) => node.kind === 'cpa_account')).toBe(
      false
    )
  })

  test('selecting a CPA account keeps only its rows', () => {
    const result = buildDashboardFlowData(rows, 'quota', {
      role: 'root',
      selectedNodes: [{ kind: 'cpa_account', id: 'cpa_account:a@x' }],
    })
    expect(result.summary.quota).toBe(60)
  })

  test('CPA account labels are masked with other sensitive labels', () => {
    const result = buildDashboardFlowData(rows, 'quota', {
      role: 'root',
      maskSensitive: true,
    })
    const node = result.flow.nodes.find((item) => item.id === 'cpa_account:a@x')
    expect(node?.label).not.toContain('a@x')
  })
})

describe('dashboard channel filter params', () => {
  const range = { start_timestamp: 1, end_timestamp: 2 }

  test('passes a trimmed channel and omits an empty one', () => {
    expect(
      buildQueryParams(range, { time_granularity: 'day', channel: ' cpa ' })
        .channel
    ).toBe('cpa')
    expect(
      'channel' in
        buildQueryParams(range, { time_granularity: 'day', channel: '  ' })
    ).toBe(false)
  })
})

describe('errorRateFromSuccessRate', () => {
  test('derives and clamps the error rate', () => {
    expect(errorRateFromSuccessRate(97.5)).toBeCloseTo(2.5)
    expect(errorRateFromSuccessRate(100)).toBe(0)
    expect(errorRateFromSuccessRate(120)).toBe(0)
  })

  test('returns NaN when there is no sample', () => {
    expect(errorRateFromSuccessRate(undefined)).toBeNaN()
    expect(errorRateFromSuccessRate(null)).toBeNaN()
    expect(errorRateFromSuccessRate(Number.NaN)).toBeNaN()
  })
})
