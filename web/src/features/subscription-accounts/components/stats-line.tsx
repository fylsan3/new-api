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
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { toIntlLocale } from '@/i18n/languages'
import { formatCurrencyFromUSD } from '@/lib/currency'
import {
  formatCompactNumber,
  formatNumber,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'

import { credentialCount, lastRequestAt } from '../lib/card'
import type { CpaAccount, CpaCardUsage } from '../types'

type StatsLineProps = {
  account: CpaAccount
  usage: CpaCardUsage | null
  collecting: boolean
}

/**
 * One-line summary: last request · success / failed · spend. Details open in
 * a floating popover so the card layout never moves.
 */
export function StatsLine(props: StatsLineProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const cred = props.account.credential ?? {}
  const success = credentialCount(cred.success)
  const failed = credentialCount(cred.failed)
  const last = lastRequestAt(props.account)
  const fromQueue = (props.account.last_request_at ?? 0) > 0
  const spend = props.usage?.cost_usd ?? null

  return (
    <Popover>
      <PopoverTrigger
        openOnHover
        delay={200}
        render={
          <button
            type='button'
            className='text-muted-foreground block w-full cursor-default truncate text-left text-[11px]'
          />
        }
      >
        {t('Last request')}{' '}
        {last ? formatTimestampRelative(last, 'seconds', locale) : '-'}
        {' · '}
        {t('Success')}{' '}
        <span className='border-b border-dashed border-current'>
          {formatCompactNumber(success, locale)}
        </span>
        {' / '}
        <span className='border-b border-dashed border-current text-red-600 dark:text-red-400'>
          {formatCompactNumber(failed, locale)}
        </span>
        {' · '}
        {t('Spend')}{' '}
        <span className='border-b border-dashed border-current'>
          {spend === null
            ? '-'
            : formatCurrencyFromUSD(spend, { compact: true })}
        </span>
      </PopoverTrigger>
      <PopoverContent align='start' className='w-72 space-y-1 text-xs'>
        <Row
          label={t('Last request')}
          value={
            last
              ? `${formatTimestampToDate(last)}${fromQueue ? '' : ` (${t('about, 10-minute buckets')})`}`
              : '-'
          }
        />
        <Row
          label={t('Success / failed (since CPA start)')}
          value={`${formatNumber(success)} / ${formatNumber(failed)}`}
        />
        {props.usage ? (
          <>
            <Row
              label={t('Requests (30 days)')}
              value={formatNumber(props.usage.requests)}
            />
            <Row
              label={t('Tokens (30 days)')}
              value={formatNumber(props.usage.tokens)}
            />
            <Row
              label={t('Spend (30 days, base price)')}
              value={formatCurrencyFromUSD(props.usage.cost_usd)}
            />
          </>
        ) : null}
        <p className='text-muted-foreground pt-1'>
          {props.collecting
            ? t(
                'Spend is estimated from New API model prices without group ratios. It is not billed.'
              )
            : t(
                'Spend needs "Collect usage" turned on for this CPA. Success/failed counts reset when CPA restarts.'
              )}
        </p>
      </PopoverContent>
    </Popover>
  )
}

function Row(props: { label: string; value: string }) {
  return (
    <div className='flex justify-between gap-3'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-right font-medium'>{props.value}</span>
    </div>
  )
}
