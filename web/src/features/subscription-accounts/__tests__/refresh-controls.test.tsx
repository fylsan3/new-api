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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { RefreshControls } from '../components/refresh-controls'

const settings = {
  auto_refresh_enabled: false,
  auto_refresh_interval_minutes: 10,
  refresh_all_cooldown_seconds: 60,
  last_refresh_all_at: 0,
  proxy_monthly_cost_usd: 0,
  rotation_enabled: false,
  rotation_work_hours: 6,
  rotation_rest_minutes: 120,
  rotation_jitter_minutes: 20,
  rotation_max_rest_percent: 30,
  rotation_min_online: 2,
  rotation_excluded: '',
  rotation_show_badges: true,
  rotation_wake_429_enabled: false,
  rotation_wake_429_minutes: 10,
  rotation_wake_429_count: 20,
  rotation_wake_rpm_enabled: false,
  rotation_wake_rpm: 10,
  rotation_wake_batch: 1,
  rotation_wake_cooldown_minutes: 5,
  rotation_wake_hold_minutes: 30,
  rotation_wake_rest: true,
  rotation_wake_shift: false,
  rotation_night_enabled: false,
  rotation_night_start: '01:00',
  rotation_night_end: '07:00',
  rotation_night_max_rest_percent: 60,
  rotation_shift_enabled: false,
  rotation_day_shift_start: '08:00',
  rotation_day_shift_end: '02:00',
  rotation_night_shift_start: '20:00',
  rotation_night_shift_end: '14:00',
  rotation_shift_jitter_minutes: 30,
  rotation_shifts: '',
  rotation_limit_enabled: false,
  rotation_limit_minutes: 10,
  rotation_limit_count: 10,
  rotation_limit_rest_minutes: 30,
  rotation_quota_enabled: false,
  rotation_quota_percent: 90,
  rotation_daily_enabled: false,
  rotation_daily_hours: 16,
  rotation_enabled_at: 0,
  rotation_batch_size: 1,
  rotation_batch_gap_minutes: 5,
  rotation_capacity_enabled: true,
  rotation_capacity_rpm: 8,
  rotation_capacity_tpm: 0,
  rotation_capacity_429_minutes: 5,
  rotation_dry_run: false,
  auth_stop_enabled: false,
  alert_wecom_enabled: false,
  alert_dingtalk_enabled: false,
  alert_on_socks: true,
  alert_on_auth: true,
  alert_on_cpa: true,
  alert_on_rotation: true,
  has_wecom_webhook: false,
  has_dingtalk_webhook: false,
  has_dingtalk_secret: false,
}

function renderControls() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RefreshControls
        settings={settings}
        running={false}
        serverNow={Math.floor(Date.now() / 1000)}
      />
    </QueryClientProvider>
  )
  return screen.getByRole('button', { name: /Refresh all/ })
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('RefreshControls settings popover', () => {
  it('click refreshes but does not open the settings', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { scheduled: 0, spread_ms: 0 } },
    })
    const button = renderControls()
    await act(async () => {
      fireEvent.click(button)
    })
    expect(post).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Refresh all settings')).toBeNull()
  })

  it('opens only after hovering 5 seconds', async () => {
    vi.useFakeTimers()
    const button = renderControls()
    act(() => {
      fireEvent.pointerEnter(button, { pointerType: 'mouse' })
      fireEvent.mouseEnter(button)
      fireEvent.mouseMove(button)
    })
    act(() => {
      vi.advanceTimersByTime(4000)
    })
    expect(screen.queryByText('Refresh all settings')).toBeNull()
    act(() => {
      vi.advanceTimersByTime(1500)
    })
    expect(screen.getByText('Refresh all settings')).toBeTruthy()
  })
})
