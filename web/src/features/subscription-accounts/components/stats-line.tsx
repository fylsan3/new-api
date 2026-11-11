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

import {
  FAIL_CATEGORIES,
  FAIL_CATEGORY_KEYS,
  compactCount,
  credentialCount,
  groupFailReasons,
  lastRequestAt,
  sevenDayCounts,
} from '../lib/card'
import type { CpaAccount, CpaCardUsage } from '../types'

type StatsLineProps = {
  account: CpaAccount
  usage: CpaCardUsage | null
  /** Usage queue collected for this CPA: counts come from New API (7 days). */
  collecting?: boolean
}

/**
 * One-line summary: last request · success / failed · spend. Counts from
 * 10,000 on are shortened to k / M with a dashed underline; hovering them
 * shows the exact number. With usage collection on, success / failed are the
 * last 7 days from New API (survive CPA restarts) and hovering the failed
 * count lists the reasons; otherwise CPA's since-start counters as before.
 */
export function StatsLine(props: StatsLineProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const cred = props.account.credential ?? {}
  const seven =
    sevenDayCounts(props.usage) ??
    (props.collecting ? { success: 0, failed: 0 } : null)
  const success = seven ? seven.success : credentialCount(cred.success)
  const failed = seven ? seven.failed : credentialCount(cred.failed)
  const last = lastRequestAt(props.account)
  const spend = props.usage?.cost_usd ?? null
  const failedClass = 'text-red-600 dark:text-red-400'

  return (
    <div className='text-muted-foreground truncate text-[11px]'>
      {t('Last request')}{' '}
      {last ? formatTimestampRelative(last, 'seconds', locale) : '-'}
      {' · '}
      {seven && `${t('Last 7 days')} `}
      {t('Success')} <Count value={success} />
      {' / '}
      {seven && failed > 0 ? (
        <FailReasons usage={props.usage} failed={failed} className={failedClass} />
      ) : (
        <Count value={failed} className={failedClass} />
      )}
      {' · '}
      {t('Spend')}{' '}
      {spend === null ? '-' : formatCurrencyFromUSD(spend, { compact: true })}
    </div>
  )
}

/** Failed count with a hover list of the last-7-day reasons. */
function FailReasons(props: {
  usage: CpaCardUsage | null
  failed: number
  className?: string
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const groups = groupFailReasons(props.usage?.fail_reasons_7d)
  const { text } = compactCount(props.failed)
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
      <TooltipContent className='max-w-72'>
        <div className='mb-1 font-medium'>
          {t('Failure reasons, last 7 days ({{count}} total)', {
            count: formatNumber(props.failed, locale),
          })}
        </div>
        {FAIL_CATEGORIES.map((category) => (
          <div key={category} className='flex justify-between gap-4'>
            <span>{t(FAIL_CATEGORY_KEYS[category])}</span>
            <span className='tabular-nums'>
              {formatNumber(groups[category], locale)}
            </span>
          </div>
        ))}
        <div className='mt-1 opacity-70'>
          {t(
            'Counted since this feature went live; earlier failures have totals only, no reasons'
          )}
        </div>
      </TooltipContent>
    </Tooltip>
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
