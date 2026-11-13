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
  BLOCK_LABEL_KEYS,
  REASON_LABEL_KEYS,
  parseExcluded,
  parseShifts,
  perAccountRate,
  previewGoing,
  rateText,
  rotationOf,
} from '../lib/rotation'
import type { CpaAccount, CpaRotationState, CpaRotationSummary } from '../types'

const account = { instance_id: 1, name: 'a.json' } as CpaAccount

describe('rotation helpers', () => {
  it('finds the rotation row of a card', () => {
    const summary: CpaRotationSummary = {
      enabled: true,
      online: 1,
      resting: 0,
      queued: 0,
      off: 0,
      accounts: [
        {
          instance_id: 1,
          name: 'a.json',
          state: 'online',
          worked_hours: 4,
          today_hours: 4,
          participates: true,
        },
      ],
    }
    expect(rotationOf(summary, account)?.worked_hours).toBe(4)
    expect(rotationOf(undefined, account)).toBeNull()
  })

  it('parses the excluded list', () => {
    expect([...parseExcluded(' 1/a.json \n\n2/b.json')]).toEqual([
      '1/a.json',
      '2/b.json',
    ])
  })

  it('parses the shift list', () => {
    const shifts = parseShifts('1/a.json=day\n2/b.json = night\nbad\n3/c=x')
    expect(shifts.get('1/a.json')).toBe('day')
    expect(shifts.get('2/b.json')).toBe('night')
    expect(shifts.size).toBe(2)
  })

  it('formats rates', () => {
    expect(rateText(0)).toBe('0')
    expect(rateText(2.4)).toBe('2.4')
    expect(rateText(12.6)).toBe('13')
    expect(rateText(25_300)).toBe('25.3k')
    expect(rateText(1_250_000)).toBe('1.2M')
  })

  it('lists who a preview takes offline in its first step', () => {
    const row = (
      name: string,
      state: CpaRotationState['state']
    ): CpaRotationState => ({
      instance_id: 1,
      name,
      state,
      worked_hours: 0,
      today_hours: 0,
      participates: true,
    })
    const summary = (accounts: CpaRotationState[]): CpaRotationSummary => ({
      enabled: true,
      online: 0,
      resting: 0,
      queued: 0,
      off: 0,
      accounts,
    })
    const preview = summary([
      row('a', 'resting'),
      row('b', 'resting'),
      row('c', 'queued'),
      row('d', 'online'),
    ])
    // b rests already (e.g. left over from before): not a new departure.
    const current = summary([row('a', 'online'), row('b', 'resting')])
    expect(previewGoing(preview, current).map((r) => r.name)).toEqual(['a'])
    expect(previewGoing(preview, undefined).map((r) => r.name)).toEqual([
      'a',
      'b',
    ])
  })

  it('spreads a pool peak over the online accounts', () => {
    expect(perAccountRate(24, 4)).toBe(6)
    expect(perAccountRate(25, 3)).toBe(8.3)
    expect(perAccountRate(10, 0)).toBeNull()
  })

  it('has a label for every rule and every safety gate', () => {
    expect(Object.keys(REASON_LABEL_KEYS).sort()).toEqual([
      'daily',
      'limit',
      'quota',
      'rotation',
      'shift',
    ])
    expect(Object.keys(BLOCK_LABEL_KEYS).sort()).toEqual([
      'batch',
      'busy',
      'capacity',
      'min_online',
      'percent',
    ])
  })
})
