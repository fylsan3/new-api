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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  CpaAccount,
  CpaAlert,
  CpaInstance,
  CpaModel,
  CpaSettings,
  CpaCycleReport,
  CpaStatsReport,
  ResetClaimResult,
  SubscriptionOverview,
} from './types'

export const OVERVIEW_QUERY_KEY = ['subscription-accounts', 'overview']

const BASE = '/api/subscription-accounts'

type Envelope<T> = { success: boolean; message?: string; data: T }

async function unwrap<T>(request: Promise<{ data: Envelope<T> }>) {
  const res = await request
  return requireServerSuccess(res.data).data
}

export type CpaInstanceInput = {
  name: string
  base_url: string
  secret: string
  usage_collect_enabled?: boolean
  channel_ids?: string
}

export type AccountRef = { instance_id: number; name: string }

export function getOverview() {
  return unwrap(api.get<Envelope<SubscriptionOverview>>(`${BASE}/`))
}

export function createInstance(input: CpaInstanceInput) {
  return unwrap(api.post<Envelope<CpaInstance>>(`${BASE}/instances`, input))
}

export function updateInstance(id: number, input: CpaInstanceInput) {
  return unwrap(
    api.put<Envelope<CpaInstance>>(`${BASE}/instances/${id}`, input)
  )
}

export function deleteInstance(id: number) {
  return unwrap(api.delete<Envelope<null>>(`${BASE}/instances/${id}`))
}

export function testInstance(
  input: Partial<CpaInstanceInput> & { id?: number }
) {
  return unwrap(
    api.post<Envelope<{ credential_count: number }>>(
      `${BASE}/instances/test`,
      input
    )
  )
}

export function syncInstances() {
  return unwrap(
    api.post<Envelope<{ failures: Record<string, string> }>>(`${BASE}/sync`)
  )
}

export function refreshAll() {
  return unwrap(
    api.post<Envelope<{ scheduled: number; spread_ms: number }>>(
      `${BASE}/refresh-all`
    )
  )
}

export function updateSettings(input: Partial<CpaSettings>) {
  return unwrap(api.put<Envelope<CpaSettings>>(`${BASE}/settings`, input))
}

export function refreshAccount(ref: AccountRef) {
  return unwrap(api.post<Envelope<CpaAccount>>(`${BASE}/account/refresh`, ref))
}

export function listAccountModels(ref: AccountRef) {
  return unwrap(
    api.get<Envelope<CpaModel[]>>(`${BASE}/account/models`, { params: ref })
  )
}

export function setAccountDisabled(ref: AccountRef, disabled: boolean) {
  return unwrap(
    api.post<Envelope<null>>(`${BASE}/account/status`, { ...ref, disabled })
  )
}

// Token refresh is left to CPA's background refresher; the card no longer has
// a button for it. The POST /account/refresh-token endpoint remains available.

export function claimResetGrant(ref: AccountRef) {
  return unwrap(
    api.post<Envelope<ResetClaimResult>>(`${BASE}/account/reset-grant`, ref)
  )
}

/** Returns the unmasked proxy_url of one account (audited server-side). */
export function revealAccountProxy(ref: AccountRef) {
  return unwrap(
    api.post<Envelope<{ proxy_url: string }>>(`${BASE}/account/proxy`, ref)
  )
}

export const STATS_QUERY_KEY = ['subscription-accounts', 'stats']

export function getStats(days: number) {
  return unwrap(
    api.get<Envelope<CpaStatsReport>>(`${BASE}/stats`, { params: { days } })
  )
}

export const CYCLES_QUERY_KEY = ['subscription-accounts', 'cycles']

/** Weekly quota periods of every Claude account (cycle comparison tab). */
export function getCycles() {
  return unwrap(api.get<Envelope<CpaCycleReport>>(`${BASE}/cycles`))
}

export function listAlerts() {
  return unwrap(api.get<Envelope<CpaAlert[]>>(`${BASE}/alerts`))
}

/** Acknowledges the given alerts, or every open one when ids is omitted. */
export function acknowledgeAlerts(ids?: number[]) {
  return unwrap(
    api.post<Envelope<{ acknowledged: number }>>(
      `${BASE}/alerts/ack`,
      ids && ids.length ? { ids } : { all: true }
    )
  )
}

export const ALERTS_QUERY_KEY = ['subscription-accounts', 'alerts']
