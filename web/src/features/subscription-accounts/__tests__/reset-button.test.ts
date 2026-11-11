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

import { resetButtonHint } from '../lib/quota'
import type { QuotaWindow } from '../types'

const NOW = Date.parse('2026-11-11T06:00:00Z')

function grant(extra: Record<string, unknown> = {}) {
  return {
    id: 'g1',
    resets_left: 1,
    resets_total: 1,
    paused: false,
    usable_now: true,
    use_requires_limit: false,
    starts_at: '2026-11-01T00:00:00Z',
    ends_at: '2026-12-01T00:00:00Z',
    ...extra,
  }
}

function block(extra: Record<string, unknown> = {}, grants = [grant()]) {
  return { eligible: true, at_limit: false, cooldown_until: null, grants, ...extra }
}

const NONE: QuotaWindow[] = []

describe('resetButtonHint', () => {
  it('no hint when a grant is usable now', () => {
    expect(resetButtonHint(block(), NONE, NOW)).toBeNull()
    expect(resetButtonHint(undefined, NONE, NOW)).toBeNull()
  })

  it('ineligible and cooldown come first', () => {
    expect(resetButtonHint(block({ eligible: false }), NONE, NOW)).toEqual({
      kind: 'ineligible',
    })
    expect(
      resetButtonHint(block({ cooldown_until: '2026-11-12T01:30:00Z' }), NONE, NOW)
    ).toEqual({ kind: 'cooldown', until: '2026-11-12T01:30:00Z' })
    // A cooldown in the past is ignored.
    expect(
      resetButtonHint(block({ cooldown_until: '2026-11-10T00:00:00Z' }), NONE, NOW)
    ).toBeNull()
  })

  it('per-grant reasons in order', () => {
    const one = (g: Record<string, unknown>) =>
      resetButtonHint(block({}, [grant(g)]), NONE, NOW)
    expect(one({ paused: true })).toEqual({ kind: 'paused' })
    expect(one({ starts_at: '2026-11-20T00:00:00Z' })).toEqual({
      kind: 'not_started',
      startsAt: '2026-11-20T00:00:00Z',
    })
    expect(one({ ends_at: '2026-11-10T00:00:00Z' })).toEqual({ kind: 'expired' })
    expect(one({ usable_now: false })).toEqual({ kind: 'not_usable' })
    expect(one({ use_requires_limit: true })).toEqual({ kind: 'not_limited' })
  })

  it('limit seen on chat counts as at limit', () => {
    const b = block({}, [grant({ use_requires_limit: true })])
    const limited: QuotaWindow[] = [
      { id: 'seven-day', labelKey: '7-day limit', usedPercent: 100, resetsAt: null, limited: true },
    ]
    expect(resetButtonHint(b, limited, NOW)).toBeNull()
    expect(resetButtonHint({ ...b, at_limit: true }, NONE, NOW)).toBeNull()
  })

  it('exhausted grants are skipped; any usable grant clears the hint', () => {
    const b = block({}, [
      grant({ id: 'a', resets_left: 0 }),
      grant({ id: 'b', paused: true }),
      grant({ id: 'c' }),
    ])
    expect(resetButtonHint(b, NONE, NOW)).toBeNull()
    const c = block({}, [grant({ id: 'a', paused: true }), grant({ id: 'b', usable_now: false })])
    expect(resetButtonHint(c, NONE, NOW)).toEqual({ kind: 'paused' })
  })
})
