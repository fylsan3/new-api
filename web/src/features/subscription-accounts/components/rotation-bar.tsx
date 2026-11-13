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
import { BedDouble, Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import type { CpaRotationSummary } from '../types'
import { EmergencyStopButton } from './emergency-stop'

/** Header line of the pool rotation (design draft section 3, F). */
export function RotationBar(props: {
  summary: CpaRotationSummary | undefined
  enabled: boolean
  onSettings: () => void
}) {
  const { t } = useTranslation()
  const s = props.summary
  return (
    <div className='flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-md border px-3 py-1.5 text-sm'>
      <BedDouble
        className={cn(
          'size-4',
          props.enabled
            ? 'text-sky-600 dark:text-sky-400'
            : 'text-muted-foreground'
        )}
      />
      <span className='font-medium'>{t('Account rotation')}</span>
      {props.enabled ? (
        <span className='text-muted-foreground text-xs'>
          {t('Online')} <b className='text-foreground'>{s?.online ?? 0}</b>
          {' · '}
          {t('Resting')}{' '}
          <b className='text-sky-700 dark:text-sky-300'>{s?.resting ?? 0}</b>
          {' · '}
          {t('Queued')} <b className='text-foreground'>{s?.queued ?? 0}</b>
          {' · '}
          {t('Disabled')} <b className='text-foreground'>{s?.off ?? 0}</b>
          {s?.dry_run ? (
            <>
              {'　'}
              <b className='text-amber-700 dark:text-amber-300'>
                {t('Dry run: {{n}} would be resting, nobody is taken offline', {
                  n: s.would_rest ?? 0,
                })}
              </b>
            </>
          ) : null}
          {s?.night ? (
            <>
              {'　'}
              <b className='text-indigo-700 dark:text-indigo-300'>
                {t('Night mode')}
              </b>
            </>
          ) : null}
          {s?.last_wake_at && Date.now() / 1000 - s.last_wake_at < 3600 ? (
            <>
              {'　'}
              {t('Pool busy, woke accounts early at {{time}}', {
                time: dayjs.unix(s.last_wake_at).format('HH:mm'),
              })}
            </>
          ) : null}
          {s?.next_online ? (
            <>
              {'　'}
              {t('Next back online')}{' '}
              <b className='text-foreground'>
                {dayjs.unix(s.next_online).format('HH:mm')}
              </b>
            </>
          ) : null}
        </span>
      ) : (
        <span className='text-muted-foreground text-xs'>{t('Off')}</span>
      )}
      <span className='flex-1' />
      {props.enabled && <EmergencyStopButton />}
      <Button size='xs' variant='outline' onClick={props.onSettings}>
        <Settings2 />
        {t('Rotation settings')}
      </Button>
    </div>
  )
}
