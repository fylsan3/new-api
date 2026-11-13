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
import type {
  CpaAccount,
  CpaRate,
  CpaRotationState,
  CpaRotationSummary,
  RestReason,
  RotationBlock,
  ShiftKind,
} from '../types'

/** Rotation view of one card, or null when the loop has not run yet. */
export function rotationOf(
  summary: CpaRotationSummary | undefined,
  account: CpaAccount
): CpaRotationState | null {
  if (!summary) return null
  return (
    summary.accounts.find(
      (row) =>
        row.instance_id === account.instance_id && row.name === account.name
    ) ?? null
  )
}

export function rotationKey(account: { instance_id: number; name: string }) {
  return `${account.instance_id}/${account.name}`
}

/** Parses the excluded list (one `${instance_id}/${name}` per line). */
export function parseExcluded(raw: string | undefined): Set<string> {
  return new Set(
    (raw ?? '')
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
  )
}

/** Parses the shift list (one `${instance_id}/${name}=day|night|all`). */
export function parseShifts(raw: string | undefined): Map<string, ShiftKind> {
  const out = new Map<string, ShiftKind>()
  for (const line of (raw ?? '').split('\n')) {
    const at = line.indexOf('=')
    if (at < 0) continue
    const value = line.slice(at + 1).trim()
    if (value === 'day' || value === 'night' || value === 'all') {
      out.set(line.slice(0, at).trim(), value)
    }
  }
  return out
}

/** Translation keys of the shift badge. */
export const SHIFT_LABEL_KEYS: Record<ShiftKind, string> = {
  day: 'Day shift',
  night: 'Night shift',
  all: 'All day',
}

/** Translation keys of the resting notice, per reason. */
export const REST_REASON_KEYS: Record<RestReason, string> = {
  rotation: 'Resting after {{hours}} h of work',
  shift: 'Off shift, resting',
  limit: 'Rate limited (429) too often, resting',
  quota: 'Quota almost used up, resting until reset',
  daily: 'Daily working hours reached, resting',
}

/** Translation keys of the queued notice, per reason. */
export const QUEUE_REASON_KEYS: Record<RestReason, string> = {
  rotation: 'Due for a rest, queued #{{pos}} (worked {{hours}} h)',
  shift: 'Off shift, queued #{{pos}}',
  limit: 'Rate limited often, queued #{{pos}}',
  quota: 'Quota almost used up, queued #{{pos}}',
  daily: 'Daily hours reached, queued #{{pos}}',
}

/** Short name of each rule, for lists (preview, settings). */
export const REASON_LABEL_KEYS: Record<RestReason, string> = {
  rotation: 'Worked long enough',
  shift: 'Off shift',
  limit: 'Too many 429s',
  quota: 'Quota almost used up',
  daily: 'Daily hours reached',
}

/** Why a queued account is still online (safety gates). */
export const BLOCK_LABEL_KEYS: Record<RotationBlock, string> = {
  min_online: 'would drop below the minimum online',
  percent: 'the share allowed to rest is full',
  batch: 'one at a time, waiting for the next step',
  capacity: 'the accounts left could not carry the load',
  busy: 'the pool is busy',
}

/**
 * Accounts a preview would take offline in its first step: resting in the
 * preview and not resting right now.
 */
export function previewGoing(
  preview: CpaRotationSummary,
  current: CpaRotationSummary | undefined
): CpaRotationState[] {
  const resting = new Set(
    (current?.accounts ?? [])
      .filter((row) => row.state === 'resting')
      .map(rotationKey)
  )
  return preview.accounts.filter(
    (row) => row.state === 'resting' && !resting.has(rotationKey(row))
  )
}

/** Peak of a pool spread over its online accounts; null with nobody online. */
export function perAccountRate(peak: number, online: number): number | null {
  if (online <= 0) return null
  return Math.round((peak / online) * 10) / 10
}

/** RPM / TPM of one card, keyed like usage (instance / auth_index). */
export function rateOf(
  rates: Record<string, CpaRate> | undefined,
  account: CpaAccount
): CpaRate | null {
  const authIndex = account.credential?.auth_index
  if (!rates || typeof authIndex !== 'string' || !authIndex) return null
  return rates[`${account.instance_id}/${authIndex}`] ?? null
}

/** Short rate text: integers as is, below 10 one decimal. */
export function rateText(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  if (value >= 1_000_000) return `${Math.trunc(value / 100_000) / 10}M`
  if (value >= 10_000) return `${Math.trunc(value / 100) / 10}k`
  if (value < 10 && !Number.isInteger(value)) return value.toFixed(1)
  return String(Math.round(value))
}
