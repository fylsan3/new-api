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
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatNumber, formatTimestampRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

import { compactCount, credentialCount, lastRequestAt } from '../lib/card'
import type { CpaAccount, CpaCardUsage } from '../types'

type StatsLineProps = {
  account: CpaAccount
  usage: CpaCardUsage | null
}

/**
 * One-line summary: last request · success / failed · spend. Counts from
 * 10,000 on are shortened to k / M with a dashed underline; hovering them
 * shows the exact number.
 */
export function StatsLine(props: StatsLineProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const cred = props.account.credential ?? {}
  const success = credentialCount(cred.success)
  const failed = credentialCount(cred.failed)
  const last = lastRequestAt(props.account)
  const spend = props.usage?.cost_usd ?? null

  return (
    <div className='text-muted-foreground truncate text-[11px]'>
      {t('Last request')}{' '}
      {last ? formatTimestampRelative(last, 'seconds', locale) : '-'}
      {' · '}
      {t('Success')} <Count value={success} />
      {' / '}
      <Count value={failed} className='text-red-600 dark:text-red-400' />
      {' · '}
      {t('Spend')}{' '}
      {spend === null ? '-' : formatCurrencyFromUSD(spend, { compact: true })}
    </div>
  )
}

/** A count; shortened ones get a dashed underline and an exact-number tooltip. */
function Count(props: { value: number; className?: string }) {
  const { i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { text, compact } = compactCount(props.value)
  if (!compact) return <span className={props.className}>{text}</span>
  return (
    <Tooltip>
      <TooltipTrigger
        delay={150}
        render={
          <span
            className={cn(
              'cursor-help border-b border-dashed border-current',
              props.className
            )}
          />
        }
      >
        {text}
      </TooltipTrigger>
      <TooltipContent>{formatNumber(props.value, locale)}</TooltipContent>
    </Tooltip>
  )
}
