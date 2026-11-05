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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { resetCountdown } from '../lib/quota'

/** The countdown re-renders once a minute; that is its display precision. */
const TICK_MS = 60_000

type ResetCountdownLabelProps = {
  resetsAt: string | null | undefined
  /** Quota data is stale: grey the text so an old time is not trusted. */
  stale?: boolean
}

/**
 * "· 2h 14m until reset" next to a quota label (design option 1, 2026-11-05).
 * Renders nothing when the reset time is unknown.
 */
export function ResetCountdownLabel(props: ResetCountdownLabelProps) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!props.resetsAt) return
    const timer = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => clearInterval(timer)
  }, [props.resetsAt])

  const left = resetCountdown(props.resetsAt, now)
  if (!left) return null

  let text: string
  switch (left.kind) {
    case 'soon':
      text = t('Resetting soon')
      break
    case 'days':
      text = t('Resets in {{days}}d {{hours}}h', {
        days: left.days,
        hours: left.hours,
      })
      break
    case 'hours':
      text = t('Resets in {{hours}}h {{minutes}}m', {
        hours: left.hours,
        minutes: left.minutes,
      })
      break
    case 'minutes':
      text = t('Resets in {{minutes}}m', { minutes: left.minutes })
      break
  }

  return (
    <span
      className={cn(
        'text-muted-foreground/80',
        props.stale && 'text-muted-foreground/50'
      )}
    >
      {' · '}
      {text}
    </span>
  )
}
