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
import type { CpaCycleRow } from '../types'

/** Window keys → labels (5-hour windows do not take part). */
export const CYCLE_WINDOW_KEYS: Record<string, string> = {
  seven_day: '7 days · all models',
  seven_day_opus: '7 days · Opus',
  seven_day_sonnet: '7 days · Sonnet',
  seven_day_fable: '7 days · Fable',
}

/**
 * True when row i (newer) and row i+1 (older) must not be compared: the
 * older one ended on a plan change, or the plans differ.
 */
export function cycleBreakAfter(rows: CpaCycleRow[], i: number): boolean {
  const newer = rows[i]
  const older = rows[i + 1]
  if (!newer || !older) return false
  if (older.end_reason === 'plan_change') return true
  return !!newer.plan_type && !!older.plan_type && newer.plan_type !== older.plan_type
}

/** "Used at end" cell: a 429 counts as 100%. */
export function cycleUsedText(row: CpaCycleRow): string {
  if (row.hit_limit) return '100%'
  if (row.used_percent === null) return '-'
  return `${Math.round(row.used_percent)}%`
}

/** Translation key of the "based on" cell, null when there is no reading. */
export function cycleBasisKey(row: CpaCycleRow): string | null {
  if (row.hit_limit) return '429 used up, most accurate'
  if (row.used_percent === null) return null
  return row.percent_source === 'chat' ? 'Chat' : 'Refresh'
}
