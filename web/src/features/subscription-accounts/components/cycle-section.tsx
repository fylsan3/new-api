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
import { useQuery } from '@tanstack/react-query'
import { CalendarRange } from 'lucide-react'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import dayjs from '@/lib/dayjs'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import { CYCLES_QUERY_KEY, getCycles } from '../api'
import { accountDisplayName } from '../lib/pool'
import { PLAN_LABEL_KEYS } from '../lib/quota'
import {
  CYCLE_WINDOW_KEYS,
  cycleBreakAfter,
  cycleBasisKey,
  cycleUsedText,
} from '../lib/cycle'
import type { CpaCycleAccount } from '../types'
import { Section } from './stats-panel'

const END_REASON_KEYS: Record<string, string> = {
  natural: 'Natural reset',
  manual_reset: 'Manual reset',
  plan_change: 'Plan changed',
}

function day(ts: number) {
  return dayjs(ts * 1000).format('MM-DD')
}

function planLabel(
  plan: string | undefined,
  t: (key: string) => string
): string {
  if (!plan) return ''
  return PLAN_LABEL_KEYS[plan] ? t(PLAN_LABEL_KEYS[plan]) : plan
}

/** Statistics page: weekly quota periods of one account and window. */
export function CycleSection() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({
    queryKey: CYCLES_QUERY_KEY,
    queryFn: getCycles,
    staleTime: 60_000,
  })
  const accounts = useMemo<CpaCycleAccount[]>(
    () => (query.data?.accounts ?? []).filter((a) => a.windows.length > 0),
    [query.data]
  )
  const [accountKey, setAccountKey] = useState('')
  const [windowKey, setWindowKey] = useState('seven_day')

  const account =
    accounts.find((a) => `${a.instance_id}/${a.name}` === accountKey) ??
    accounts[0]
  useEffect(() => {
    if (!account) return
    if (!account.windows.some((w) => w.window === windowKey)) {
      setWindowKey(account.windows[0]?.window ?? 'seven_day')
    }
  }, [account, windowKey])
  const rows =
    account?.windows.find((w) => w.window === windowKey)?.cycles ?? []
  const minPercent = query.data?.min_percent ?? 20

  return (
    <Section
      icon={<CalendarRange />}
      title={t('Cycle comparison')}
      description={t(
        'Estimated full quota = tokens in the period ÷ used percent at its end. A clear drop against the previous period may mean the official quota was quietly reduced.'
      )}
    >
      <div className='flex flex-wrap items-center gap-3 border-b px-3 py-2 text-xs sm:px-5'>
        <label className='flex items-center gap-1.5'>
          <span className='text-muted-foreground'>{t('Account')}</span>
          <NativeSelect
            size='sm'
            className='max-w-72'
            value={account ? `${account.instance_id}/${account.name}` : ''}
            onChange={(e) => setAccountKey(e.target.value)}
            disabled={accounts.length === 0}
          >
            {accounts.map((a) => {
              const plan = planLabel(a.plan_type, t)
              return (
                <NativeSelectOption
                  key={`${a.instance_id}/${a.name}`}
                  value={`${a.instance_id}/${a.name}`}
                >
                  {accountDisplayName(a)}
                  {plan ? ` (${plan})` : ''}
                </NativeSelectOption>
              )
            })}
          </NativeSelect>
        </label>
        <label className='flex items-center gap-1.5'>
          <span className='text-muted-foreground'>{t('Window')}</span>
          <NativeSelect
            size='sm'
            value={windowKey}
            onChange={(e) => setWindowKey(e.target.value)}
            disabled={!account}
          >
            {(account?.windows ?? []).map((w) => (
              <NativeSelectOption key={w.window} value={w.window}>
                {t(CYCLE_WINDOW_KEYS[w.window] ?? w.window)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </label>
        <span className='text-muted-foreground'>
          {t(
            'Only usage that went through CPA; direct use on the web or desktop app is not included'
          )}
        </span>
      </div>
      {query.isLoading ? (
        <Skeleton className='m-3 h-40' />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Period')}</TableHead>
              <TableHead>{t('Ended by')}</TableHead>
              <TableHead className='text-right'>
                {t('Tokens in period')}
              </TableHead>
              <TableHead className='text-right'>{t('Cache read')}</TableHead>
              <TableHead className='text-right'>{t('Used at end')}</TableHead>
              <TableHead className='text-right'>
                {t('Estimated full (tokens)')}
              </TableHead>
              <TableHead className='text-right'>
                {t('vs previous')}
              </TableHead>
              <TableHead>{t('Based on')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className='text-muted-foreground text-center text-xs'
                >
                  {t(
                    'No periods yet. The first row appears after the next refresh; a full period takes up to 7 days.'
                  )}
                </TableCell>
              </TableRow>
            )}
            {rows.map((row, i) => (
              <Fragment key={row.id}>
                <TableRow>
                  <TableCell className='whitespace-nowrap tabular-nums'>
                    {day(row.start_ts)} → {day(row.end_ts)}
                    {row.backfilled && (
                      <span className='ml-1.5 rounded bg-amber-50 px-1 text-[11px] text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'>
                        {t('Backfilled, for reference only')}
                      </span>
                    )}
                  </TableCell>
                  <TableCell
                    className={cn(
                      'text-xs whitespace-nowrap',
                      row.end_reason === 'manual_reset' &&
                        'text-violet-700 dark:text-violet-300'
                    )}
                  >
                    {row.open
                      ? t('In progress')
                      : t(END_REASON_KEYS[row.end_reason] ?? row.end_reason)}
                  </TableCell>
                  <TableCell className='text-right tabular-nums'>
                    {formatNumber(row.tokens, locale)}
                  </TableCell>
                  <TableCell className='text-muted-foreground text-right tabular-nums'>
                    {formatNumber(row.cache_read_tokens, locale)}
                  </TableCell>
                  <TableCell className='text-right tabular-nums'>
                    {cycleUsedText(row)}
                  </TableCell>
                  <TableCell className='text-right tabular-nums'>
                    {row.estimated_full !== null ? (
                      `≈ ${formatNumber(row.estimated_full, locale)}`
                    ) : (
                      <span className='text-muted-foreground text-xs'>
                        {row.used_percent !== null &&
                        row.used_percent < minPercent
                          ? t('Not estimated (used < {{n}}%)', {
                              n: minPercent,
                            })
                          : '-'}
                      </span>
                    )}
                  </TableCell>
                  <TableCell
                    className={cn(
                      'text-right tabular-nums',
                      row.change_percent !== null &&
                        row.change_percent <= -5 &&
                        'text-red-600 dark:text-red-400'
                    )}
                  >
                    {row.change_percent === null
                      ? '-'
                      : `${row.change_percent > 0 ? '+' : ''}${Math.round(row.change_percent)}%`}
                  </TableCell>
                  <TableCell className='text-xs whitespace-nowrap'>
                    {cycleBasisKey(row) === null ? (
                      '-'
                    ) : (
                      <span
                        className={
                          row.hit_limit
                            ? 'font-medium text-emerald-700 dark:text-emerald-400'
                            : 'text-muted-foreground'
                        }
                      >
                        {t(cycleBasisKey(row) ?? '')}
                        {!row.hit_limit && row.open && row.percent_at
                          ? ` ${dayjs(row.percent_at * 1000).format('HH:mm')}`
                          : ''}
                      </span>
                    )}
                  </TableCell>
                </TableRow>
                {cycleBreakAfter(rows, i) && (
                  <TableRow className='hover:bg-transparent'>
                    <TableCell
                      colSpan={8}
                      className='text-muted-foreground bg-muted/40 py-1 text-center text-xs'
                    >
                      {t(
                        '{{date}} plan changed from {{from}} to {{to}}; rows above and below are not compared',
                        {
                          date: day(rows[i + 1].end_ts),
                          from: planLabel(rows[i + 1].plan_type, t) || '-',
                          to: planLabel(row.plan_type, t) || '-',
                        }
                      )}
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            ))}
          </TableBody>
        </Table>
      )}
    </Section>
  )
}
