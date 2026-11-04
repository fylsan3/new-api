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
import { Link } from '@tanstack/react-router'
import { ArrowRight, KeySquare } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatCompactNumber, formatTimestampRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  OVERVIEW_QUERY_KEY,
  STATS_QUERY_KEY,
  getOverview,
  getStats,
} from '../api'
import {
  buildPoolHealth,
  dailyOf,
  failRate,
  localDayStart,
  tightestAccounts,
  type TightAccount,
} from '../lib/pool'
import { quotaLevel } from '../lib/quota'
import { CpaAlertBanner } from './alert-banner'

const LEVEL_BAR: Record<string, string> = {
  ok: 'bg-emerald-500',
  warn: 'bg-amber-500',
  danger: 'bg-red-500',
  unknown: 'bg-muted-foreground/40',
}

function formatDuration(
  sec: number,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  if (sec < 3600) {
    return t('about {{n}} min', { n: Math.max(1, Math.round(sec / 60)) })
  }
  if (sec < 86400) return t('about {{n}} h', { n: Math.round(sec / 360) / 10 })
  return t('about {{n}} d', { n: Math.round(sec / 8640) / 10 })
}

function Metric(props: {
  label: string
  children: React.ReactNode
  hint?: React.ReactNode
}) {
  return (
    <div className='bg-muted/40 min-w-0 rounded-md px-3 py-2.5'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-0.5 text-lg font-semibold tabular-nums'>
        {props.children}
      </div>
      {props.hint != null && (
        <div className='text-muted-foreground mt-0.5 truncate text-xs'>
          {props.hint}
        </div>
      )}
    </div>
  )
}

function TightList(props: {
  title: string
  items: TightAccount[]
  resetHint: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground mb-1.5 text-xs'>{props.title}</div>
      {props.items.length === 0 ? (
        <div className='text-muted-foreground text-xs'>{t('No data')}</div>
      ) : (
        <ul className='space-y-1.5'>
          {props.items.map((item) => {
            const level = quotaLevel(item.remaining)
            let extra = ''
            if (item.exhaustInSec !== null) {
              extra = formatDuration(item.exhaustInSec, t)
            } else if (props.resetHint && item.resetsAt) {
              extra = `${t('Resets at')} ${new Date(item.resetsAt).toLocaleString()}`
            }
            return (
              <li key={item.key} className='text-xs'>
                <div className='flex justify-between gap-2'>
                  <span className='truncate'>
                    {item.name}
                    {item.plan ? ` · ${t(item.plan)}` : ''}
                  </span>
                  <span className='text-muted-foreground shrink-0 tabular-nums'>
                    {Math.round(item.remaining)}%{extra ? ` · ${extra}` : ''}
                  </span>
                </div>
                <div className='bg-muted mt-0.5 h-1 overflow-hidden rounded-full'>
                  <div
                    className={cn('h-full rounded-full', LEVEL_BAR[level])}
                    style={{ width: `${item.remaining}%` }}
                  />
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

/**
 * Overview panel for root users: open alerts + account pool health. Reads
 * only what New API already cached; opening it never calls CPA.
 */
export function SubscriptionPoolPanel() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const overview = useQuery({
    queryKey: OVERVIEW_QUERY_KEY,
    queryFn: getOverview,
    staleTime: 30_000,
    retry: false,
  })
  const stats = useQuery({
    queryKey: [...STATS_QUERY_KEY, 2],
    queryFn: () => getStats(2),
    staleTime: 60_000,
    retry: false,
  })

  const data = overview.data
  const nowSec = data?.now ?? Math.floor(Date.now() / 1000)
  const health = useMemo(
    () => buildPoolHealth(data?.accounts ?? [], nowSec),
    [data, nowSec]
  )
  const points = stats.data?.quota_points
  const tight5h = useMemo(
    () =>
      tightestAccounts(data?.accounts ?? [], 'five-hour', points ?? [], nowSec),
    [data, points, nowSec]
  )
  const tight7d = useMemo(
    () =>
      tightestAccounts(data?.accounts ?? [], 'seven-day', points ?? [], nowSec),
    [data, points, nowSec]
  )

  if (overview.isError) return null
  if (overview.isLoading) return <Skeleton className='h-48 w-full rounded-lg' />
  if (!data || (health.total === 0 && !data.alerts?.length)) return null

  const today = localDayStart(nowSec)
  const todayPoint = dailyOf(stats.data?.daily, today)
  const yesterdayPoint = dailyOf(stats.data?.daily, today - 86400)
  const collecting = data.instances.some((i) => i.usage_collect_enabled)
  const lastRefresh = Math.max(
    0,
    ...data.accounts.map((a) => a.quota_checked_at ?? a.updated_at ?? 0)
  )
  const todayRate = failRate(todayPoint)
  const yesterdayRate = failRate(yesterdayPoint)

  return (
    <div className='space-y-3'>
      <CpaAlertBanner alerts={data.alerts ?? []} />
      <div className='overflow-hidden rounded-lg border'>
        <div className='flex items-start justify-between gap-2 border-b px-4 py-3 sm:px-5'>
          <div className='flex items-center gap-2'>
            <IconBadge tone='info' size='sm'>
              <KeySquare />
            </IconBadge>
            <div>
              <div className='text-sm font-semibold'>
                {t('Subscription account pool')}
              </div>
              <div className='text-muted-foreground text-xs'>
                {lastRefresh
                  ? t('Data from last refresh · {{time}}', {
                      time: formatTimestampRelative(
                        lastRefresh,
                        'seconds',
                        locale
                      ),
                    })
                  : t('Not refreshed yet')}
                {' · '}
                {t('Opening the overview does not send any request')}
              </div>
            </div>
          </div>
          <Button
            variant='ghost'
            size='sm'
            render={<Link to='/subscription-accounts' />}
          >
            {t('Manage')}
            <ArrowRight data-icon='inline-end' />
          </Button>
        </div>
        <div className='space-y-4 p-4 sm:p-5'>
          <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
            <Metric
              label={t('Available accounts')}
              hint={t('Disabled {{d}} · Cooling {{c}}', {
                d: health.disabled,
                c: health.cooling,
              })}
            >
              {health.available}
              <span className='text-muted-foreground text-sm font-normal'>
                {' '}
                / {health.total}
              </span>
            </Metric>
            <Metric
              label={t('SOCKS status')}
              hint={t('Not configured = chats of that account go out directly')}
            >
              <span className='flex flex-wrap gap-x-2 text-sm font-medium'>
                <span className='text-emerald-600 dark:text-emerald-400'>
                  {t('OK {{n}}', { n: health.socks.ok })}
                </span>
                <span className='text-red-600 dark:text-red-400'>
                  {t('Down {{n}}', { n: health.socks.failed })}
                </span>
                <span className='text-muted-foreground'>
                  {t('Not set {{n}}', { n: health.socks.missing })}
                </span>
              </span>
            </Metric>
            {collecting ? (
              <Metric
                label={t('Today success / failed')}
                hint={t('Fail rate {{today}} · yesterday {{yesterday}}', {
                  today: todayRate === null ? '-' : `${todayRate.toFixed(1)}%`,
                  yesterday:
                    yesterdayRate === null
                      ? '-'
                      : `${yesterdayRate.toFixed(1)}%`,
                })}
              >
                {formatCompactNumber(
                  (todayPoint?.requests ?? 0) - (todayPoint?.failed ?? 0),
                  locale
                )}
                <span className='text-sm font-normal text-red-600 dark:text-red-400'>
                  {' '}
                  / {formatCompactNumber(todayPoint?.failed ?? 0, locale)}
                </span>
              </Metric>
            ) : (
              <Metric
                label={t('Success / failed since CPA start')}
                hint={t('Turn on usage collection for daily numbers')}
              >
                {formatCompactNumber(health.cpaSuccess, locale)}
                <span className='text-sm font-normal text-red-600 dark:text-red-400'>
                  {' '}
                  / {formatCompactNumber(health.cpaFailed, locale)}
                </span>
              </Metric>
            )}
            <Metric
              label={t('Reset grants left')}
              hint={
                health.earliestEnds
                  ? t('{{count}} end first on {{date}}', {
                      count: health.earliestEndsCount,
                      date: health.earliestEnds,
                    })
                  : undefined
              }
            >
              {health.resetsLeft}
            </Metric>
          </div>
          <div className='grid gap-4 md:grid-cols-2'>
            <TightList
              title={t('Tightest 3 on the 5-hour limit')}
              items={tight5h}
              resetHint={false}
            />
            <TightList
              title={t('Tightest 3 on the 7-day limit')}
              items={tight7d}
              resetHint
            />
          </div>
        </div>
      </div>
    </div>
  )
}
