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
  CpaDailyPoint,
  CpaQuotaPoint,
  CpaStatsReport,
} from '../types'
import { credentialCount, socksStateOf, type SocksState } from './card'
import {
  PLAN_LABEL_KEYS,
  buildQuotaWindows,
  buildResetGrants,
  remainingPercent,
} from './quota'

export type PoolHealth = {
  total: number
  available: number
  disabled: number
  cooling: number
  socks: Record<SocksState, number>
  resetsLeft: number
  /** Earliest end date among grants that still have resets, and how many end then. */
  earliestEnds: string | null
  earliestEndsCount: number
  /** Cumulative CPA counters (since CPA start), used when usage is not collected. */
  cpaSuccess: number
  cpaFailed: number
}

function isCooling(account: CpaAccount, nowSec: number): boolean {
  const cred = account.credential ?? {}
  if (cred.unavailable === true) return true
  const retry = cred.next_retry_after
  if (typeof retry === 'string' && retry) {
    const ts = Date.parse(retry)
    if (Number.isFinite(ts) && ts / 1000 > nowSec) return true
  }
  return false
}

/** Pool summary computed only from the cached snapshots (no CPA requests). */
export function buildPoolHealth(
  accounts: CpaAccount[],
  nowSec: number
): PoolHealth {
  const claude = accounts.filter((a) => a.is_claude)
  const health: PoolHealth = {
    total: claude.length,
    available: 0,
    disabled: 0,
    cooling: 0,
    socks: { ok: 0, failed: 0, missing: 0 },
    resetsLeft: 0,
    earliestEnds: null,
    earliestEndsCount: 0,
    cpaSuccess: 0,
    cpaFailed: 0,
  }
  const endsCount = new Map<string, number>()
  for (const account of claude) {
    const cred = account.credential ?? {}
    if (cred.disabled === true) health.disabled++
    else if (isCooling(account, nowSec)) health.cooling++
    else health.available++
    health.socks[socksStateOf(account)]++
    health.cpaSuccess += credentialCount(cred.success)
    health.cpaFailed += credentialCount(cred.failed)
    const grants = buildResetGrants(account.reset_grants)
    if (!grants) continue
    health.resetsLeft += grants.resetsLeft
    for (const grant of grants.grants) {
      if (grant.resetsLeft <= 0 || !grant.endsAt) continue
      const day = grant.endsAt.slice(0, 10)
      endsCount.set(day, (endsCount.get(day) ?? 0) + grant.resetsLeft)
    }
  }
  const days = [...endsCount.keys()].sort()
  if (days.length) {
    health.earliestEnds = days[0]
    health.earliestEndsCount = endsCount.get(days[0]) ?? 0
  }
  return health
}

export type TightAccount = {
  key: string
  name: string
  plan: string | null
  remaining: number
  resetsAt: string | null
  /** Estimated seconds until the window is used up, when the trend allows it. */
  exhaustInSec: number | null
}

export function accountDisplayName(account: {
  name: string
  credential?: { email?: string; label?: string }
  email?: string
}): string {
  const raw =
    account.credential?.email ||
    account.credential?.label ||
    account.email ||
    account.name
  return raw.includes('@') ? raw.split('@')[0] : raw
}

const TREND_WINDOW_SEC = 3600

/**
 * Linear burn estimate from the last hour of quota points: how long until
 * used reaches 100%. Null when there is no upward trend or not enough points.
 */
export function estimateExhaustSec(
  points: CpaQuotaPoint[],
  field: 'five_hour' | 'seven_day',
  nowSec: number
): number | null {
  const recent = points
    .filter((p) => p.ts >= nowSec - TREND_WINDOW_SEC && p[field] != null)
    .sort((a, b) => a.ts - b.ts)
  if (recent.length < 2) return null
  const first = recent[0]
  const last = recent.at(-1)
  if (!last) return null
  const dt = last.ts - first.ts
  const du = (last[field] ?? 0) - (first[field] ?? 0)
  if (dt <= 0 || du <= 0) return null
  const left = 100 - (last[field] ?? 0)
  if (left <= 0) return 0
  return Math.round((left / du) * dt)
}

/** The `limit` accounts with the least remaining quota in one window. */
export function tightestAccounts(
  accounts: CpaAccount[],
  windowId: 'five-hour' | 'seven-day',
  points: CpaQuotaPoint[],
  nowSec: number,
  limit = 3
): TightAccount[] {
  const field = windowId === 'five-hour' ? 'five_hour' : 'seven_day'
  const out: TightAccount[] = []
  for (const account of accounts) {
    if (!account.is_claude || account.credential?.disabled === true) continue
    const window = buildQuotaWindows(account.usage).find(
      (w) => w.id === windowId
    )
    const remaining = remainingPercent(window?.usedPercent ?? null)
    if (remaining === null) continue
    const own = points.filter(
      (p) => p.instance_id === account.instance_id && p.name === account.name
    )
    let exhaust = estimateExhaustSec(own, field, nowSec)
    const resetsAtSec = window?.resetsAt
      ? Date.parse(window.resetsAt) / 1000
      : 0
    // Only meaningful if it runs out before the window resets anyway.
    if (exhaust !== null && resetsAtSec && nowSec + exhaust >= resetsAtSec) {
      exhaust = null
    }
    out.push({
      key: `${account.instance_id}/${account.name}`,
      name: accountDisplayName(account),
      plan: account.plan_type
        ? (PLAN_LABEL_KEYS[account.plan_type] ?? null)
        : null,
      remaining,
      resetsAt: window?.resetsAt ?? null,
      exhaustInSec: exhaust,
    })
  }
  return out.sort((a, b) => a.remaining - b.remaining).slice(0, limit)
}

/** Local-midnight seconds for a timestamp. */
export function localDayStart(sec: number): number {
  const d = new Date(sec * 1000)
  d.setHours(0, 0, 0, 0)
  return Math.floor(d.getTime() / 1000)
}

export function dailyOf(
  daily: CpaDailyPoint[] | undefined,
  daySec: number
): CpaDailyPoint | null {
  return (daily ?? []).find((p) => p.day === daySec) ?? null
}

export function failRate(point: CpaDailyPoint | null): number | null {
  if (!point || point.requests <= 0) return null
  return (point.failed / point.requests) * 100
}

export type CostSummary = {
  planCostUSD: number
  /** Accounts whose plan has no official price (team / generic max / free). */
  unknownPlanAccounts: number
  proxyCostUSD: number
  revenueUSD: number
  officialCostUSD: number
  /** revenue / (plan + proxy); null when cost is 0. */
  payback: number | null
}

export function buildCostSummary(
  report: CpaStatsReport | undefined
): CostSummary {
  const summary: CostSummary = {
    planCostUSD: 0,
    unknownPlanAccounts: 0,
    proxyCostUSD: 0,
    revenueUSD: 0,
    officialCostUSD: 0,
    payback: null,
  }
  if (!report) return summary
  for (const account of report.accounts) {
    summary.planCostUSD += account.plan_cost_usd
    if (!account.plan_cost_known) summary.unknownPlanAccounts++
    summary.proxyCostUSD += account.proxy_cost_usd
    summary.officialCostUSD += account.cost_usd
  }
  for (const instance of report.instances) {
    summary.revenueUSD += instance.revenue_usd
  }
  const cost = summary.planCostUSD + summary.proxyCostUSD
  summary.payback = cost > 0 ? summary.revenueUSD / cost : null
  return summary
}
