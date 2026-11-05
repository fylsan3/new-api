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
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { RefreshControls } from '../components/refresh-controls'

const settings = {
  auto_refresh_enabled: false,
  auto_refresh_interval_minutes: 10,
  refresh_all_cooldown_seconds: 60,
  last_refresh_all_at: 0,
  proxy_monthly_cost_usd: 0,
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
