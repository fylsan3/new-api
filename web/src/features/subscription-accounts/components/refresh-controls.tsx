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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import { OVERVIEW_QUERY_KEY, refreshAll, updateSettings } from '../api'
import type { CpaSettings } from '../types'

const MIN_COOLDOWN_SECONDS = 60
const MIN_INTERVAL_MINUTES = 10
// Settings popover opens only after hovering the button this long, so a
// quick pass over the button does not pop it up.
const SETTINGS_HOVER_DELAY_MS = 5000

type RefreshControlsProps = {
  settings: CpaSettings
  running: boolean
  serverNow: number
}

function useCooldownRemaining(settings: CpaSettings, serverNow: number) {
  // Clock skew between server and browser, measured once per server
  // timestamp. Recomputing it on every render would cancel out the elapsed
  // time and freeze the countdown.
  const offset = useMemo(
    () => serverNow - Math.floor(Date.now() / 1000),
    [serverNow]
  )
  const compute = () => {
    if (!settings.last_refresh_all_at) return 0
    const now = Math.floor(Date.now() / 1000) + offset
    return Math.max(
      0,
      settings.last_refresh_all_at + settings.refresh_all_cooldown_seconds - now
    )
  }
  const [remaining, setRemaining] = useState(compute)
  useEffect(() => {
    setRemaining(compute())
    const timer = window.setInterval(() => setRemaining(compute()), 1000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    settings.last_refresh_all_at,
    settings.refresh_all_cooldown_seconds,
    offset,
  ])
  return remaining
}

export function RefreshControls(props: RefreshControlsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const remaining = useCooldownRemaining(props.settings, props.serverNow)
  const [cooldownDraft, setCooldownDraft] = useState(
    String(Math.round(props.settings.refresh_all_cooldown_seconds / 60))
  )
  const [intervalDraft, setIntervalDraft] = useState(
    String(props.settings.auto_refresh_interval_minutes)
  )

  const [settingsOpen, setSettingsOpen] = useState(false)

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })

  const refreshMutation = useMutation({
    mutationFn: refreshAll,
    onSuccess: (data) => {
      toast.success(
        t('Refreshing {{count}} accounts over {{seconds}} seconds', {
          count: data.scheduled,
          seconds: Math.round(data.spread_ms / 1000),
        })
      )
      invalidate()
      // Pick up results after the staggered requests settle.
      window.setTimeout(invalidate, data.spread_ms + 5000)
    },
    onError: (error) => {
      handleServerError(error)
      invalidate()
    },
  })

  const settingsMutation = useMutation({
    mutationFn: updateSettings,
    onSuccess: (data) => {
      toast.success(t('Saved'))
      setCooldownDraft(
        String(Math.round(data.refresh_all_cooldown_seconds / 60))
      )
      setIntervalDraft(String(data.auto_refresh_interval_minutes))
      invalidate()
    },
    onError: (error) => handleServerError(error),
  })

  const saveCooldown = () => {
    const minutes = Math.max(1, Math.floor(Number(cooldownDraft) || 1))
    settingsMutation.mutate({
      refresh_all_cooldown_seconds: Math.max(
        MIN_COOLDOWN_SECONDS,
        minutes * 60
      ),
    })
  }

  const saveInterval = () => {
    const minutes = Math.max(
      MIN_INTERVAL_MINUTES,
      Math.floor(Number(intervalDraft) || MIN_INTERVAL_MINUTES)
    )
    settingsMutation.mutate({ auto_refresh_interval_minutes: minutes })
  }

  const blocked = remaining > 0 || props.running || refreshMutation.isPending
  let label = t('Refresh all')
  if (props.running || refreshMutation.isPending) {
    label = t('Refreshing...')
  } else if (remaining > 0) {
    label = t('Refresh all ({{seconds}}s)', { seconds: remaining })
  }

  return (
    <div className='flex flex-wrap items-center gap-3'>
      <div className='flex items-center gap-2 text-sm'>
        <Switch
          id='cpa-auto-refresh'
          checked={props.settings.auto_refresh_enabled}
          disabled={settingsMutation.isPending}
          onCheckedChange={(checked) =>
            settingsMutation.mutate({ auto_refresh_enabled: checked })
          }
        />
        <Label htmlFor='cpa-auto-refresh'>{t('Background refresh')}</Label>
        <Input
          className='h-8 w-16'
          type='number'
          min={MIN_INTERVAL_MINUTES}
          value={intervalDraft}
          onChange={(event) => setIntervalDraft(event.target.value)}
          onBlur={saveInterval}
          aria-label={t('Interval (minutes)')}
        />
        <span className='text-muted-foreground'>{t('min')}</span>
      </div>

      <Popover
        open={settingsOpen}
        onOpenChange={(open, details) => {
          // The trigger is also the "Refresh all" button. Base UI opens a
          // popover on click/focus by default; only a 5s hover may open it.
          if (
            open &&
            (details.reason === 'trigger-press' ||
              details.reason === 'trigger-focus')
          ) {
            return
          }
          setSettingsOpen(open)
        }}
      >
        <PopoverTrigger
          openOnHover
          delay={SETTINGS_HOVER_DELAY_MS}
          render={
            <Button
              size='sm'
              disabled={blocked}
              onClick={() => refreshMutation.mutate()}
            />
          }
        >
          {props.running || refreshMutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <RefreshCw />
          )}
          {label}
        </PopoverTrigger>
        <PopoverContent align='end' className='w-64 space-y-2'>
          <div className='text-sm font-medium'>{t('Refresh all settings')}</div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Requests for all accounts are spread over 5-10 seconds. Minimum cooldown is 1 minute.'
            )}
          </p>
          <div className='flex items-center gap-2'>
            <Label htmlFor='cpa-cooldown' className='shrink-0 text-xs'>
              {t('Cooldown (minutes)')}
            </Label>
            <Input
              id='cpa-cooldown'
              className='h-8'
              type='number'
              min={1}
              value={cooldownDraft}
              onChange={(event) => setCooldownDraft(event.target.value)}
            />
            <Button
              size='sm'
              variant='outline'
              disabled={settingsMutation.isPending}
              onClick={saveCooldown}
            >
              {t('Save')}
            </Button>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  )
}
