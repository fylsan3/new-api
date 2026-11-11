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
 * missing: no valid socks5 proxy (grey). With no proxy_url at all, read-only
 * quota queries still go through CPA directly; an invalid proxy_url blocks.
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

/** True when a proxy_url is set but rejected (not socks5/socks5h). */
export function isProxyBlocked(account: CpaAccount): boolean {
  return (account.proxy_error ?? '').startsWith(BLOCKED_PREFIX)
}

/** Translation key for service.ErrCpaProxyBlocked. */
export const PROXY_BLOCKED_KEY =
  'socks5 proxy is invalid, blocked, request not sent'

/** Error text for display: the blocked sentinel becomes a translation key. */
export function quotaErrorKey(error: string): string | null {
  return error.startsWith(BLOCKED_PREFIX) ? PROXY_BLOCKED_KEY : null
}

/**
 * Quota data is live when the account has a working SOCKS proxy, or no
 * proxy_url at all (test accounts left without one on purpose: quota queries
 * then go through CPA directly). A failed or invalid proxy makes it stale.
 */
export function quotaLive(account: CpaAccount): boolean {
  const socks = socksStateOf(account)
  if (socks === 'ok') return true
  return socks === 'missing' && !account.proxy_url && !isProxyBlocked(account)
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

/** Counts at or above this are shortened to k / M on the card. */
export const COMPACT_COUNT_THRESHOLD = 10_000

/**
 * Card count text: below 10,000 the exact number; from 10,000 on "12.3k" /
 * "1.2M" (always k/M, regardless of UI language). Returns compact=true when
 * shortened so the caller can offer the exact number on hover.
 */
export function compactCount(value: number): {
  text: string
  compact: boolean
} {
  const n = Number.isFinite(value) ? value : 0
  if (Math.abs(n) < COMPACT_COUNT_THRESHOLD) {
    return { text: String(n), compact: false }
  }
  const [div, unit] = Math.abs(n) >= 1_000_000 ? [1_000_000, 'M'] : [1_000, 'k']
  // Truncate (not round) so 9,999,999 never shows as "10000k".
  const scaled = Math.trunc((n / div) * 10) / 10
  return { text: `${scaled}${unit}`, compact: true }
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

/**
 * Failure categories of the card tooltip / details (design 2026-11-11,
 * option C). CPA records 500 when it has no status, so "upstream" may hold a
 * few local errors.
 */
export type FailCategory = 'rate' | 'proxy' | 'upstream' | 'auth' | 'other'

export const FAIL_CATEGORIES: FailCategory[] = [
  'rate',
  'proxy',
  'upstream',
  'auth',
  'other',
]

/** Translation keys of the five failure categories. */
export const FAIL_CATEGORY_KEYS = {
  rate: '429 rate limited (quota used up / too frequent)',
  proxy: '502 / connection failed (mostly SOCKS)',
  upstream: '529 / 5xx upstream busy or error',
  auth: '401 / 403 authorization invalid',
  other: 'Other',
} as const

export function failCategoryOf(statusCode: number): FailCategory {
  if (statusCode === 429) return 'rate'
  if (statusCode === 502 || statusCode === 0) return 'proxy'
  if (statusCode === 401 || statusCode === 403) return 'auth'
  if (statusCode >= 500 && statusCode <= 599) return 'upstream'
  return 'other'
}

/** Sums fail_reasons_7d into the five categories (all present, 0 when none). */
export function groupFailReasons(
  reasons: CpaCardUsage['fail_reasons_7d']
): Record<FailCategory, number> {
  const out: Record<FailCategory, number> = {
    rate: 0,
    proxy: 0,
    upstream: 0,
    auth: 0,
    other: 0,
  }
  for (const reason of reasons ?? []) {
    out[failCategoryOf(reason.status_code)] += reason.count
  }
  return out
}

/**
 * Last-7-day success / failed from New API's hourly usage table; null when
 * the usage queue is not collected for this account (caller falls back to
 * CPA's since-start counters).
 */
export function sevenDayCounts(
  usage: CpaCardUsage | null
): { success: number; failed: number } | null {
  if (!usage || typeof usage.requests_7d !== 'number') return null
  const failed = usage.failed_7d ?? 0
  return { success: Math.max(0, usage.requests_7d - failed), failed }
}
