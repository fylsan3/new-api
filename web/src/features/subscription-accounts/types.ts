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
  created_at: number
  updated_at: number
}

export type CpaSettings = {
  auto_refresh_enabled: boolean
  auto_refresh_interval_minutes: number
  refresh_all_cooldown_seconds: number
  last_refresh_all_at: number
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
}

export type CpaAccount = {
  instance_id: number
  name: string
  credential?: CpaCredential
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
  updated_at: number
}

export type SubscriptionOverview = {
  instances: CpaInstance[]
  accounts: CpaAccount[]
  settings: CpaSettings
  refresh_all_running: boolean
  now: number
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
}
