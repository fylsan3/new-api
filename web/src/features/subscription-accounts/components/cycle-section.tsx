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
import { CalendarRange, ChevronDown, ChevronRight } from 'lucide-react'
import { Fragment, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import { CYCLES_QUERY_KEY, getCycles } from '../api'
import {
  CYCLE_DROP_PERCENT,
  CYCLE_WINDOW_KEYS,
  cycleBreakAfter,
  cycleBasisKey,
  cycleOverview,
  cycleUsedText,
} from '../lib/cycle'
import { accountDisplayName } from '../lib/pool'
import { PLAN_LABEL_KEYS } from '../lib/quota'
import type { CpaCycleAccount, CpaCycleRow } from '../types'
import { Section } from './stats-panel'

const END_REASON_KEYS: Record<string, string> = {
  natural: 'Natural reset',
  manual_reset: 'Manual reset',
  plan_change: 'Plan changed',
}

const OVERVIEW_COLUMNS = 7

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

function changeText(change: number | null): string {
  if (change === null) return '-'
  return `${change > 0 ? '+' : ''}${Math.round(change)}%`
}

/**
 * Statistics page: weekly quota periods, one row per account so every
 * account can be compared at a glance (user decision 2026-11-12). The
 * biggest drop comes first; a row opens the periods of that account.
 */
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
  const [windowChoice, setWindowChoice] = useState('seven_day')
  const [plan, setPlan] = useState('')
  const [onlyDrops, setOnlyDrops] = useState(false)
  const [open, setOpen] = useState<Set<string>>(() => new Set())

  // Windows any account has, in the fixed order of the labels.
  const windows = useMemo(() => {
    const seen = new Set(
      accounts.flatMap((a) => a.windows.map((w) => w.window))
    )
    const known = Object.keys(CYCLE_WINDOW_KEYS).filter((k) => seen.has(k))
    return [...known, ...[...seen].filter((k) => !CYCLE_WINDOW_KEYS[k]).sort()]
  }, [accounts])
  const windowKey = windows.includes(windowChoice)
    ? windowChoice
    : (windows[0] ?? 'seven_day')
  const plans = useMemo(
    () =>
      [
        ...new Set(accounts.map((a) => a.plan_type ?? '').filter(Boolean)),
      ].sort(),
    [accounts]
  )
  const all = useMemo(
    () => cycleOverview(accounts, windowKey),
    [accounts, windowKey]
  )
  const dropped = all.filter((row) => row.dropped).length
  const rows = all.filter(
    (row) =>
      (!plan || row.account.plan_type === plan) && (!onlyDrops || row.dropped)
  )
  const minPercent = query.data?.min_percent ?? 20

  const toggle = (key: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  return (
    <Section
      icon={<CalendarRange />}
      title={t('Cycle comparison')}
      description={t(
        'Estimated full quota = tokens in the period ÷ used percent at its end. A clear drop against the previous period may mean the official quota was quietly reduced.'
      )}
    >
      <div className='flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-3 py-2 text-xs sm:px-5'>
        <label className='flex items-center gap-1.5'>
          <span className='text-muted-foreground'>{t('Window')}</span>
          <NativeSelect
            size='sm'
            value={windowKey}
            onChange={(e) => setWindowChoice(e.target.value)}
            disabled={windows.length === 0}
          >
            {windows.map((w) => (
              <NativeSelectOption key={w} value={w}>
                {t(CYCLE_WINDOW_KEYS[w] ?? w)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </label>
        <label className='flex items-center gap-1.5'>
          <span className='text-muted-foreground'>{t('Plan')}</span>
          <NativeSelect
            size='sm'
            value={plan}
            onChange={(e) => setPlan(e.target.value)}
            disabled={plans.length === 0}
          >
            <NativeSelectOption value=''>{t('All plans')}</NativeSelectOption>
            {plans.map((p) => (
              <NativeSelectOption key={p} value={p}>
                {planLabel(p, t)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </label>
        <label className='flex cursor-pointer items-center gap-1.5'>
          <Switch checked={onlyDrops} onCheckedChange={setOnlyDrops} />
          <span>
            {t('Only drops of {{n}}% or more', { n: CYCLE_DROP_PERCENT })}
          </span>
          <span
            className={cn(
              'tabular-nums',
              dropped > 0
                ? 'font-medium text-red-600 dark:text-red-400'
                : 'text-muted-foreground'
            )}
          >
            ({dropped})
          </span>
        </label>
        <span className='text-muted-foreground'>
          {t(
            'One row per account, biggest drop first. Click a row for all its periods. Only usage that went through CPA is counted.'
          )}
        </span>
      </div>
      {query.isLoading ? (
        <Skeleton className='m-3 h-40' />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Account')}</TableHead>
              <TableHead>{t('Plan')}</TableHead>
              <TableHead>{t('Latest period')}</TableHead>
              <TableHead className='text-right'>{t('Used at end')}</TableHead>
              <TableHead className='text-right'>
                {t('Estimated full (tokens)')}
              </TableHead>
              <TableHead className='text-right'>{t('vs previous')}</TableHead>
              <TableHead>{t('Based on')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={OVERVIEW_COLUMNS}
                  className='text-muted-foreground text-center text-xs'
                >
                  {all.length === 0
                    ? t(
                        'No periods yet. The first row appears after the next refresh; a full period takes up to 7 days.'
                      )
                    : t('No account matches the filters')}
                </TableCell>
              </TableRow>
            )}
            {rows.map((row) => {
              const latest = row.latest
              const expanded = open.has(row.key)
              return (
                <Fragment key={row.key}>
                  <TableRow
                    className='cursor-pointer'
                    aria-expanded={expanded}
                    onClick={() => toggle(row.key)}
                  >
                    <TableCell className='max-w-72'>
                      <span className='flex items-center gap-1.5'>
                        {expanded ? (
                          <ChevronDown className='text-muted-foreground size-3.5 flex-none' />
                        ) : (
                          <ChevronRight className='text-muted-foreground size-3.5 flex-none' />
                        )}
                        <span className='truncate'>
                          {accountDisplayName(row.account)}
                        </span>
                        <span className='text-muted-foreground flex-none text-xs tabular-nums'>
                          {t('{{n}} periods', { n: row.rows.length })}
                        </span>
                      </span>
                    </TableCell>
                    <TableCell className='text-xs whitespace-nowrap'>
                      {planLabel(row.account.plan_type, t) || '-'}
                    </TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>
                      {latest ? (
                        <>
                          {day(latest.start_ts)} → {day(latest.end_ts)}
                          {latest.open && (
                            <span className='text-muted-foreground ml-1.5 text-xs'>
                              {t('In progress')}
                            </span>
                          )}
                        </>
                      ) : (
                        '-'
                      )}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {latest ? cycleUsedText(latest) : '-'}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {latest ? (
                        <EstimatedCell
                          row={latest}
                          locale={locale}
                          minPercent={minPercent}
                        />
                      ) : (
                        '-'
                      )}
                    </TableCell>
                    <TableCell
                      className={cn(
                        'text-right tabular-nums',
                        row.dropped &&
                          'font-medium text-red-600 dark:text-red-400'
                      )}
                    >
                      {changeText(row.change)}
                    </TableCell>
                    <TableCell className='text-xs whitespace-nowrap'>
                      {latest ? <BasisCell row={latest} /> : '-'}
                    </TableCell>
                  </TableRow>
                  {expanded && (
                    <TableRow className='hover:bg-transparent'>
                      <TableCell
                        colSpan={OVERVIEW_COLUMNS}
                        className='bg-muted/30 p-0'
                      >
                        <CycleDetail
                          rows={row.rows}
                          locale={locale}
                          minPercent={minPercent}
                        />
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              )
            })}
          </TableBody>
        </Table>
      )}
    </Section>
  )
}

function EstimatedCell(props: {
  row: CpaCycleRow
  locale: string | undefined
  minPercent: number
}) {
  const { t } = useTranslation()
  const row = props.row
  if (row.estimated_full !== null) {
    return <span>{`≈ ${formatNumber(row.estimated_full, props.locale)}`}</span>
  }
  return (
    <span className='text-muted-foreground text-xs'>
      {row.used_percent !== null && row.used_percent < props.minPercent
        ? t('Not estimated (used < {{n}}%)', { n: props.minPercent })
        : '-'}
    </span>
  )
}

function BasisCell(props: { row: CpaCycleRow }) {
  const { t } = useTranslation()
  const row = props.row
  const key = cycleBasisKey(row)
  if (key === null) return <>-</>
  return (
    <span
      className={
        row.hit_limit
          ? 'font-medium text-emerald-700 dark:text-emerald-400'
          : 'text-muted-foreground'
      }
    >
      {t(key)}
      {!row.hit_limit && row.open && row.percent_at
        ? ` ${dayjs(row.percent_at * 1000).format('HH:mm')}`
        : ''}
    </span>
  )
}

/** Every period of one account and window, newest first. */
function CycleDetail(props: {
  rows: CpaCycleRow[]
  locale: string | undefined
  minPercent: number
}) {
  const { t } = useTranslation()
  const rows = props.rows
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className='pl-9'>{t('Period')}</TableHead>
          <TableHead>{t('Ended by')}</TableHead>
          <TableHead className='text-right'>{t('Tokens in period')}</TableHead>
          <TableHead className='text-right'>{t('Cache read')}</TableHead>
          <TableHead className='text-right'>{t('Used at end')}</TableHead>
          <TableHead className='text-right'>
            {t('Estimated full (tokens)')}
          </TableHead>
          <TableHead className='text-right'>{t('vs previous')}</TableHead>
          <TableHead>{t('Based on')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, i) => (
          <Fragment key={row.id}>
            <TableRow>
              <TableCell className='pl-9 whitespace-nowrap tabular-nums'>
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
                {formatNumber(row.tokens, props.locale)}
              </TableCell>
              <TableCell className='text-muted-foreground text-right tabular-nums'>
                {formatNumber(row.cache_read_tokens, props.locale)}
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                {cycleUsedText(row)}
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                <EstimatedCell
                  row={row}
                  locale={props.locale}
                  minPercent={props.minPercent}
                />
              </TableCell>
              <TableCell
                className={cn(
                  'text-right tabular-nums',
                  row.change_percent !== null &&
                    row.change_percent <= -CYCLE_DROP_PERCENT &&
                    'text-red-600 dark:text-red-400'
                )}
              >
                {changeText(row.change_percent)}
              </TableCell>
              <TableCell className='text-xs whitespace-nowrap'>
                <BasisCell row={row} />
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
  )
}
