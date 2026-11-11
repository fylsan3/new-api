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
export type CpaInstance = {
  id: number
  name: string
  base_url: string
  has_secret: boolean
  secret: string
  last_test_at: number
  last_test_ok: boolean
  last_test_msg: string
  usage_collect_enabled: boolean
  channel_ids: string
  usage_last_poll_at: number
  usage_last_record_at: number
  usage_last_error: string
  /** 0 unknown, 1 CPA usage statistics on, 2 off. */
  usage_remote_state: number
  created_at: number
  updated_at: number
}

export type CpaSettings = {
  auto_refresh_enabled: boolean
  auto_refresh_interval_minutes: number
  refresh_all_cooldown_seconds: number
  last_refresh_all_at: number
  proxy_monthly_cost_usd: number
}

/** Credential fields mirrored from CPA `GET /credentials` (subset). */
export type CpaCredential = Record<string, unknown> & {
  name?: string
  provider?: string
  type?: string
  label?: string
  email?: string
  account?: string
  account_type?: string
  status?: string
  status_message?: string
  disabled?: boolean
  unavailable?: boolean
  auth_index?: string
  success?: number
  failed?: number
  priority?: number
  note?: string
  last_refresh?: string
  next_retry_after?: string
  modtime?: string
  updated_at?: string
  refresh_error?: string
  requires_sign_in?: boolean
  recent_requests?: unknown[]
}

export type CpaAccount = {
  instance_id: number
  name: string
  credential?: CpaCredential
  credential_synced_at?: number
  proxy_url?: string
  proxy_error?: string
  is_claude: boolean
  usage?: Record<string, unknown>
  plan_type?: string
  quota_observed_at?: number
  quota_checked_at?: number
  quota_error?: string
  quota_error_status?: number
  reset_grants?: Record<string, unknown>
  reset_grants_error?: string
  proxy_fail_streak?: number
  auto_disabled_at?: number
  last_request_at?: number
  /** Daily random plan / reset-grant refresh (unix seconds, server day). */
  auto_refresh_day?: string
  auto_refresh_plan?: number[]
  auto_refresh_done?: number
  auto_refresh_last_at?: number
  auto_refresh_error?: string
  updated_at: number
}

export type CpaFailReason = { status_code: number; count: number }

export type CpaLastFailInfo = { at: number; status_code: number; code?: string }

/** Last-30-day usage collected from the CPA usage queue. */
export type CpaCardUsage = {
  requests: number
  tokens: number
  cost_usd: number
  /** Last 7 days (design 2026-11-11, option D). */
  requests_7d?: number
  failed_7d?: number
  /** Failures by HTTP status code, most first; 0 = no status. */
  fail_reasons_7d?: CpaFailReason[]
  last_fail?: CpaLastFailInfo
}

export type CpaAlert = {
  id: number
  instance_id: number
  name: string
  kind: string
  message: string
  count: number
  created_at: number
  last_seen_at: number
}

export type SubscriptionOverview = {
  instances: CpaInstance[]
  accounts: CpaAccount[]
  settings: CpaSettings
  refresh_all_running: boolean
  now: number
  /** Keyed by `${instance_id}/${auth_index}`. */
  usage: Record<string, CpaCardUsage>
  alerts: CpaAlert[]
}

export type CpaAccountStats = {
  instance_id: number
  name: string
  auth_index: string
  email?: string
  plan_type?: string
  disabled: boolean
  requests: number
  failed: number
  tokens: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  avg_latency_ms: number
  cost_usd: number
  unpriced_requests: number
  revenue_usd: number
  plan_cost_usd: number
  plan_cost_known: boolean
  proxy_cost_usd: number
  five_hour?: number
  seven_day?: number
}

export type CpaDailyPoint = {
  day: number
  requests: number
  failed: number
  tokens: number
  cost_usd: number
  revenue_usd: number
}

export type CpaInstanceRevenue = {
  instance_id: number
  channel_ids: number[]
  revenue_usd: number
  requests: number
}

export type CpaQuotaPoint = {
  instance_id: number
  name: string
  ts: number
  five_hour?: number | null
  seven_day?: number | null
}

export type CpaStatsReport = {
  from: number
  to: number
  days: number
  accounts: CpaAccountStats[]
  daily: CpaDailyPoint[]
  instances: CpaInstanceRevenue[]
  quota_points: CpaQuotaPoint[]
  estimated: boolean
}

export type CpaModel = {
  id?: string
  display_name?: string
  type?: string
  owned_by?: string
}

export type ResetClaimResult = { result: string; grant_id?: string }

export type QuotaWindow = {
  id: string
  labelKey: string
  usedPercent: number | null
  resetsAt: string | null
  /** Anthropic rejected requests in this window (seen on a chat). */
  limited?: boolean
  /** The reset time has passed since the data was taken: shown as full. */
  resetDone?: boolean
}

export type DollarWindow = {
  key: string
  limitDollars: number | null
  usedDollars: number | null
  remainingDollars: number | null
  resetsAt: string | null
}

export type ExtraUsage = {
  isEnabled: boolean | null
  monthlyLimit: number | null
  usedCredits: number | null
  utilization: number | null
  currency: string | null
}

export type ResetGrantInfo = {
  id: string
  label: string
  resetsTotal: number
  resetsLeft: number
  endsAt: string | null
  /** Upstream flags; null when not reported. */
  usableNow?: boolean | null
  paused?: boolean | null
}

/** One weekly period on the cycle comparison tab. */
export type CpaCycleRow = {
  id: number
  start_ts: number
  end_ts: number
  open: boolean
  /** '' while open; natural | manual_reset | plan_change. */
  end_reason: string
  plan_type: string
  tokens: number
  cache_read_tokens: number
  used_percent: number | null
  percent_at: number
  /** refresh | chat */
  percent_source: string
  hit_limit: boolean
  estimated_full: number | null
  change_percent: number | null
  backfilled: boolean
}

export type CpaCycleWindow = { window: string; cycles: CpaCycleRow[] }

export type CpaCycleAccount = {
  instance_id: number
  name: string
  email?: string
  plan_type?: string
  windows: CpaCycleWindow[]
}

export type CpaCycleReport = {
  min_percent: number
  accounts: CpaCycleAccount[]
}
