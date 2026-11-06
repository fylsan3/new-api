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

import { formatTimestampRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

import { toIntlLocale } from '@/i18n/languages'

import type { EffectiveQuota } from '../lib/quota'

/** Relative time is re-rendered every 10 s so "12 s ago" keeps moving. */
const TICK_MS = 10_000

type QuotaSourceLineProps = {
  source: EffectiveQuota['source']
  observedAt: number
}

/**
 * "● From chat · 12 seconds ago" under the quota bars (design draft
 * 2026-11-06, section 1). Green dot for chat data, grey for a refresh.
 */
export function QuotaSourceLine(props: QuotaSourceLineProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [, setTick] = useState(0)

  useEffect(() => {
    if (!props.source) return
    const timer = setInterval(() => setTick((n) => n + 1), TICK_MS)
    return () => clearInterval(timer)
  }, [props.source])

  if (!props.source || !props.observedAt) return null
  const chat = props.source === 'chat'
  return (
    <div className='text-muted-foreground -mt-0.5 mb-1 flex items-center gap-1 text-[11px]'>
      <span
        className={cn(
          'inline-block size-1.5 rounded-full',
          chat ? 'bg-emerald-500' : 'bg-muted-foreground/50'
        )}
      />
      {chat ? t('From chat') : t('From refresh')}
      {' · '}
      {formatTimestampRelative(props.observedAt, 'seconds', locale)}
    </div>
  )
}
