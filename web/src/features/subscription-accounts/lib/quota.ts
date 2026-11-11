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
  DollarWindow,
  ExtraUsage,
  QuotaWindow,
  ResetGrantInfo,
} from '../types'

/** Usage windows in display order, same as the CPA management panel. */
const WINDOW_KEYS = [
  { key: 'five_hour', id: 'five-hour', labelKey: '5-hour limit' },
  { key: 'seven_day', id: 'seven-day', labelKey: '7-day limit' },
  {
    key: 'seven_day_oauth_apps',
    id: 'seven-day-oauth-apps',
    labelKey: '7-day OAuth apps',
  },
  { key: 'seven_day_opus', id: 'seven-day-opus', labelKey: '7-day Opus' },
  { key: 'seven_day_sonnet', id: 'seven-day-sonnet', labelKey: '7-day Sonnet' },
  { key: 'seven_day_cowork', id: 'seven-day-cowork', labelKey: '7-day Cowork' },
  { key: 'iguana_necktie', id: 'seven-day-fable', labelKey: '7-day Fable' },
] as const

export const PLAN_LABEL_KEYS: Record<string, string> = {
  plan_team: 'Team',
  plan_max20: 'Max 20x',
  plan_max5: 'Max 5x',
  plan_max: 'Max',
  plan_pro: 'Pro',
  plan_free: 'Free',
}

type Rec = Record<string, unknown>

function isRecord(value: unknown): value is Rec {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function toNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

function toStr(value: unknown): string | null {
  return typeof value === 'string' && value.trim() !== '' ? value : null
}

function toBool(value: unknown): boolean | null {
  return typeof value === 'boolean' ? value : null
}

function findFableLimit(usage: Rec): Rec | null {
  const limits = usage.limits
  if (!Array.isArray(limits)) return null
  const candidates = limits.filter((limit): limit is Rec => {
    if (!isRecord(limit)) return false
    const kind = (toStr(limit.kind) ?? '').trim().toLowerCase()
    const scope = isRecord(limit.scope) ? limit.scope : null
    const scopeModel = scope && isRecord(scope.model) ? scope.model : null
    const modelName = (toStr(scopeModel?.display_name) ?? '')
      .trim()
      .toLowerCase()
    const isFable = modelName === 'fable' || modelName === 'fable 5'
    return (
      kind === 'weekly_scoped' && isFable && toNumber(limit.percent) !== null
    )
  })
  return (
    candidates.find((limit) => limit.is_active === true) ??
    candidates[0] ??
    null
  )
}

function isDollarWindow(window: Rec) {
  return (
    toNumber(window.limit_dollars) !== null ||
    toNumber(window.used_dollars) !== null ||
    toNumber(window.remaining_dollars) !== null
  )
}

const DOLLAR_FIELDS = [
  'limit_dollars',
  'used_dollars',
  'remaining_dollars',
  'utilization',
  'resets_at',
]

export function buildDollarWindows(usage: Rec | undefined): DollarWindow[] {
  if (!usage) return []
  return Object.entries(usage)
    .filter(
      ([key, window]) =>
        !['five_hour', 'seven_day', 'extra_usage'].includes(key) &&
        isRecord(window) &&
        DOLLAR_FIELDS.every((field) => Object.hasOwn(window, field))
    )
    .map(([key, window]) => {
      const balance = window as Rec
      return {
        key,
        limitDollars: toNumber(balance.limit_dollars),
        usedDollars: toNumber(balance.used_dollars),
        remainingDollars: toNumber(balance.remaining_dollars),
        resetsAt: toStr(balance.resets_at),
      }
    })
}

export function buildQuotaWindows(usage: Rec | undefined): QuotaWindow[] {
  if (!usage) return []
  const windows: QuotaWindow[] = []
  const fable = findFableLimit(usage)
  const dollarKeys = new Set(buildDollarWindows(usage).map((w) => w.key))

  for (const { key, id, labelKey } of WINDOW_KEYS) {
    const window = usage[key]
    if (!isRecord(window) || !('utilization' in window)) continue
    const isCreditPool = key === 'iguana_necktie' && isDollarWindow(window)
    if (key === 'iguana_necktie' && fable && !isCreditPool) continue
    if (isCreditPool) {
      // A full dollar balance is shown in the balance rows, not as a meter.
      if (dollarKeys.has(key)) continue
      windows.push({
        id: 'cloud-session-credits',
        labelKey: 'Cloud session credits',
        usedPercent: toNumber(window.utilization),
        resetsAt: toStr(window.resets_at),
      })
      continue
    }
    windows.push({
      id,
      labelKey,
      usedPercent: toNumber(window.utilization),
      resetsAt: toStr(window.resets_at),
    })
  }

  if (fable) {
    const used = toNumber(fable.percent)
    if (used !== null) {
      windows.push({
        id: 'seven-day-fable',
        labelKey: '7-day Fable',
        usedPercent: used,
        resetsAt: toStr(fable.resets_at),
      })
    }
  }
  return windows
}

export function buildExtraUsage(usage: Rec | undefined): ExtraUsage | null {
  const extra = usage && isRecord(usage.extra_usage) ? usage.extra_usage : null
  if (!extra) return null
  return {
    isEnabled: toBool(extra.is_enabled),
    monthlyLimit: toNumber(extra.monthly_limit),
    usedCredits: toNumber(extra.used_credits),
    utilization: toNumber(extra.utilization),
    currency: toStr(extra.currency),
  }
}

export type ResetGrantSummary = {
  eligible: boolean
  resetsLeft: number
  grants: ResetGrantInfo[]
}

export function buildResetGrants(
  block: Rec | undefined
): ResetGrantSummary | null {
  if (!isRecord(block) || typeof block.eligible !== 'boolean') return null
  const raw = Array.isArray(block.grants) ? block.grants : []
  const grants: ResetGrantInfo[] = []
  for (const item of raw) {
    if (!isRecord(item) || typeof item.id !== 'string') continue
    const total = toNumber(item.resets_total) ?? 0
    const left = toNumber(item.resets_left) ?? 0
    grants.push({
      id: item.id,
      label: (toStr(item.label) ?? '').slice(0, 120),
      resetsTotal: total,
      resetsLeft: left,
      endsAt: toStr(item.ends_at),
      usableNow: toBool(item.usable_now),
      paused: toBool(item.paused),
    })
  }
  return {
    eligible: block.eligible,
    resetsLeft: grants.reduce(
      (sum, grant) => sum + Math.max(0, grant.resetsLeft),
      0
    ),
    grants,
  }
}

export type ResetCountdown =
  | { kind: 'soon' }
  | { kind: 'days'; days: number; hours: number }
  | { kind: 'hours'; hours: number; minutes: number }
  | { kind: 'minutes'; minutes: number }

/**
 * Time left until a quota window resets, for the card label. Past or
 * near-zero times read "resetting soon"; unknown times return null.
 */
export function resetCountdown(
  resetsAt: string | null | undefined,
  nowMs: number
): ResetCountdown | null {
  if (!resetsAt) return null
  const target = Date.parse(resetsAt)
  if (Number.isNaN(target)) return null
  const totalMinutes = Math.ceil((target - nowMs) / 60_000)
  if (totalMinutes <= 0) return { kind: 'soon' }
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) return { kind: 'days', days, hours }
  if (hours > 0) return { kind: 'hours', hours, minutes }
  return { kind: 'minutes', minutes }
}

/** Remaining percent = 100 - used, clamped. */
export function remainingPercent(usedPercent: number | null): number | null {
  if (usedPercent === null) return null
  return Math.min(100, Math.max(0, 100 - usedPercent))
}

// ---- passive quota seen on chats (design draft 2026-11-06, section 1) ----

/**
 * CPA records the `anthropic-ratelimit-unified-*` headers of every Claude
 * reply in credential.quota.signals (header names canonicalised). Only the
 * 5-hour and 7-day windows are carried there; utilization is a 0-1 fraction
 * and reset is unix seconds.
 */
const SIGNAL_PREFIX = 'Anthropic-Ratelimit-Unified-'
const PASSIVE_WINDOWS = [
  { tag: '5h', id: 'five-hour', labelKey: '5-hour limit' },
  { tag: '7d', id: 'seven-day', labelKey: '7-day limit' },
] as const

export type PassiveQuota = {
  observedAt: number
  windows: QuotaWindow[]
}

function signalOf(signals: Rec, name: string): string | null {
  const wanted = `${SIGNAL_PREFIX}${name}`.toLowerCase()
  for (const [key, value] of Object.entries(signals)) {
    if (key.toLowerCase() === wanted) return toStr(value)
  }
  return null
}

/** Passive quota from the CPA credential, or null when none was seen yet. */
export function buildPassiveQuota(
  credential: Rec | undefined
): PassiveQuota | null {
  const quota = credential && isRecord(credential.quota) ? credential.quota : null
  const signals = quota && isRecord(quota.signals) ? quota.signals : null
  const observedMs = Date.parse(toStr(quota?.observed_at) ?? '')
  if (!signals || Number.isNaN(observedMs)) return null
  const windows: QuotaWindow[] = []
  for (const { tag, id, labelKey } of PASSIVE_WINDOWS) {
    const fraction = toNumber(signalOf(signals, `${tag}-Utilization`))
    if (fraction === null) continue
    const resetSec = toNumber(signalOf(signals, `${tag}-Reset`))
    windows.push({
      id,
      labelKey,
      usedPercent: fraction * 100,
      resetsAt: resetSec ? new Date(resetSec * 1000).toISOString() : null,
      limited:
        (signalOf(signals, `${tag}-Status`) ?? '').toLowerCase() === 'rejected',
    })
  }
  if (windows.length === 0) return null
  return { observedAt: Math.floor(observedMs / 1000), windows }
}

export type EffectiveQuota = {
  windows: QuotaWindow[]
  /** Where the 5-hour / 7-day bars come from; null when nothing is known. */
  source: 'chat' | 'refresh' | null
  /** Seconds timestamp of that source. */
  observedAt: number
}

/**
 * Quota shown on the card: the newer of the chat-observed and the refreshed
 * 5-hour / 7-day windows; other windows (Opus, Sonnet, Fable ...) always come
 * from the refresh. A window whose reset time has passed reads as full.
 */
export function buildEffectiveQuota(
  account: CpaAccount,
  nowMs: number
): EffectiveQuota {
  const refreshed = buildQuotaWindows(account.usage)
  const refreshedAt = account.quota_observed_at ?? 0
  const passive = buildPassiveQuota(account.credential)
  let windows = refreshed
  let source: EffectiveQuota['source'] = refreshed.length ? 'refresh' : null
  let observedAt = refreshed.length ? refreshedAt : 0
  if (passive && passive.observedAt > refreshedAt) {
    const replaced = new Set(passive.windows.map((w) => w.id))
    const rest = refreshed.filter((w) => !replaced.has(w.id))
    windows = [...passive.windows, ...rest]
    source = 'chat'
    observedAt = passive.observedAt
  }
  windows = windows.map((window) => {
    const resetMs = window.resetsAt ? Date.parse(window.resetsAt) : Number.NaN
    if (Number.isNaN(resetMs) || resetMs > nowMs) return window
    return { ...window, usedPercent: 0, limited: false, resetDone: true }
  })
  return { windows, source, observedAt }
}

/** Next planned daily refresh (seconds), or null when today's are done. */
export function nextAutoRefreshAt(account: CpaAccount): number | null {
  const plan = account.auto_refresh_plan ?? []
  const done = account.auto_refresh_done ?? 0
  return done < plan.length ? plan[done] : null
}

export type QuotaLevel = 'ok' | 'warn' | 'danger' | 'unknown'

/**
 * Same tiers as the CPA management panel v1.27.2 (QuotaMeter.tsx):
 * remaining >= 70 green, >= 30 amber, < 30 red.
 */
export const QUOTA_HIGH_THRESHOLD = 70
export const QUOTA_MEDIUM_THRESHOLD = 30

export function quotaLevel(remaining: number | null): QuotaLevel {
  if (remaining === null) return 'unknown'
  if (remaining >= QUOTA_HIGH_THRESHOLD) return 'ok'
  if (remaining >= QUOTA_MEDIUM_THRESHOLD) return 'warn'
  return 'danger'
}

export function resetsLeftOf(account: CpaAccount): number {
  return buildResetGrants(account.reset_grants)?.resetsLeft ?? 0
}

/** Accounts with reset grants left go first; otherwise keep CPA order by name. */
export function sortAccounts(accounts: CpaAccount[]): CpaAccount[] {
  return accounts
    .map((account, index) => ({ account, index, left: resetsLeftOf(account) }))
    .sort((a, b) => {
      const aTop = a.left > 0 ? 1 : 0
      const bTop = b.left > 0 ? 1 : 0
      if (aTop !== bTop) return bTop - aTop
      if (aTop && a.left !== b.left) return b.left - a.left
      return a.index - b.index
    })
    .map((item) => item.account)
}
