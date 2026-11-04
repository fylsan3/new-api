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
import type { CpaAccount, CpaAlert, CpaCardUsage } from '../types'

/**
 * ok: a valid socks5 proxy worked at the last check (green).
 * failed: the proxy is configured but the last check through it failed (red).
 * missing: no valid socks5 proxy, New API refuses to send anything (grey).
 */
export type SocksState = 'ok' | 'failed' | 'missing'

/** Prefix of service.ErrCpaProxyBlocked. */
const BLOCKED_PREFIX = 'blocked:'

export function socksStateOf(account: CpaAccount): SocksState {
  const error = account.proxy_error ?? ''
  if (error.startsWith(BLOCKED_PREFIX)) return 'missing'
  if (
    (account.proxy_fail_streak ?? 0) > 0 ||
    (account.auto_disabled_at ?? 0) > 0
  ) {
    return 'failed'
  }
  if (error) return 'failed'
  return account.proxy_url ? 'ok' : 'missing'
}

export function cardUsageOf(
  usage: Record<string, CpaCardUsage> | undefined,
  account: CpaAccount
): CpaCardUsage | null {
  const authIndex = account.credential?.auth_index
  if (!usage || typeof authIndex !== 'string' || !authIndex) return null
  return usage[`${account.instance_id}/${authIndex}`] ?? null
}

/** Open alerts that belong to one account. */
export function alertsOf(alerts: CpaAlert[] | undefined, account: CpaAccount) {
  return (alerts ?? []).filter(
    (alert) =>
      alert.instance_id === account.instance_id && alert.name === account.name
  )
}

export function credentialCount(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : 0
  }
  return 0
}

/** CPA `recent_requests`: 20 buckets of 10 minutes, oldest first. */
const RECENT_BUCKET_SECONDS = 600

/**
 * Seconds timestamp of the newest request. Prefers the usage queue; falls
 * back to the newest non-empty CPA recent_requests bucket, positioned
 * relative to when the credential was synced (accurate to ~10 minutes).
 */
export function lastRequestAt(account: CpaAccount): number {
  const fromQueue = account.last_request_at ?? 0
  if (fromQueue > 0) return fromQueue
  const recent = account.credential?.recent_requests
  const syncedAt = account.credential_synced_at ?? 0
  if (!Array.isArray(recent) || !syncedAt) return 0
  for (let i = recent.length - 1; i >= 0; i--) {
    const bucket = recent[i] as Record<string, unknown> | null
    if (!bucket || typeof bucket !== 'object') continue
    const total =
      credentialCount(bucket.success) + credentialCount(bucket.failed)
    if (total > 0) {
      return syncedAt - (recent.length - 1 - i) * RECENT_BUCKET_SECONDS
    }
  }
  return 0
}
