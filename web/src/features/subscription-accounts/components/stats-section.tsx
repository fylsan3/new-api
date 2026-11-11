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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { ArrowDownUp, Coins, LineChart, Loader2, Table2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useTheme } from '@/context/theme-provider'
import { toIntlLocale } from '@/i18n/languages'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatCompactNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import {
  OVERVIEW_QUERY_KEY,
  STATS_QUERY_KEY,
  getOverview,
  getStats,
  updateSettings,
} from '../api'
import { socksStateOf, type SocksState } from '../lib/card'
import { accountDisplayName, buildCostSummary } from '../lib/pool'
import { PLAN_LABEL_KEYS } from '../lib/quota'
import type { CpaAccount, CpaAccountStats } from '../types'
import { CycleSection } from './cycle-section'
import { Section } from './stats-panel'

const RANGE_OPTIONS = [7, 14, 30] as const

type SortKey =
  | 'name'
  | 'requests'
  | 'tokens'
  | 'revenue_usd'
  | 'cost_usd'
  | 'five_hour'
  | 'seven_day'

type Row = CpaAccountStats & {
  key: string
  label: string
  socks: SocksState | null
}

const SOCKS_CLASS: Record<SocksState, string> = {
  ok: 'text-emerald-600 dark:text-emerald-400',
  failed: 'text-red-600 dark:text-red-400',
  missing: 'text-muted-foreground',
}

const SOCKS_LABEL: Record<SocksState, string> = {
  ok: 'OK',
  failed: 'Down',
  missing: 'Not set',
}

function usd(value: number, locale?: string): string {
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: 'USD',
    notation: Math.abs(value) >= 10000 ? 'compact' : 'standard',
    maximumFractionDigits: Math.abs(value) >= 100 ? 0 : 2,
  }).format(value)
}

function remaining(used: number | undefined | null): number | null {
  if (used == null) return null
  return Math.min(100, Math.max(0, 100 - used))
}

function CostMetric(props: {
  label: string
  value: string
  hint?: string
  tone?: 'good' | 'bad'
}) {
  return (
    <div className='bg-muted/40 rounded-md px-3 py-2.5'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div
        className={cn(
          'mt-0.5 text-lg font-semibold tabular-nums',
          props.tone === 'good' && 'text-emerald-600 dark:text-emerald-400',
          props.tone === 'bad' && 'text-red-600 dark:text-red-400'
        )}
      >
        {props.value}
      </div>
      {props.hint && (
        <div className='text-muted-foreground mt-0.5 text-xs'>{props.hint}</div>
      )}
    </div>
  )
}

function ProxyCostEditor(props: { value: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState(String(props.value))
  useEffect(() => setDraft(String(props.value)), [props.value])
  const mutation = useMutation({
    mutationFn: (value: number) =>
      updateSettings({ proxy_monthly_cost_usd: value }),
    onSuccess: () => {
      toast.success(t('Saved'))
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: STATS_QUERY_KEY })
    },
    onError: (error) => handleServerError(error),
  })
  const parsed = Number(draft)
  const valid = draft.trim() !== '' && Number.isFinite(parsed) && parsed >= 0
  return (
    <div className='flex items-center gap-1.5'>
      <span className='text-muted-foreground text-xs whitespace-nowrap'>
        {t('Proxy cost per account / month (USD)')}
      </span>
      <Input
        className='h-7 w-20 text-xs'
        inputMode='decimal'
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
      />
      <Button
        size='xs'
        variant='outline'
        disabled={!valid || parsed === props.value || mutation.isPending}
        onClick={() => mutation.mutate(parsed)}
      >
        {mutation.isPending && <Loader2 className='animate-spin' />}
        {t('Save')}
      </Button>
    </div>
  )
}

/** Dashboard tab: comparison table, quota trend, cost vs revenue (root only). */
export function SubscriptionStatsSection() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { resolvedTheme } = useTheme()
  const [days, setDays] = useState<number>(7)
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({
    key: 'tokens',
    desc: true,
  })
  const [themeReady, setThemeReady] = useState(false)

  useEffect(() => {
    let cancelled = false
    setThemeReady(false)
    void import('@visactor/vchart').then((m) => {
      if (cancelled) return
      m.ThemeManager.setCurrentTheme(
        resolvedTheme === 'dark' ? 'dark' : 'light'
      )
      setThemeReady(true)
    })
    return () => {
      cancelled = true
    }
  }, [resolvedTheme])

  const overview = useQuery({
    queryKey: OVERVIEW_QUERY_KEY,
    queryFn: getOverview,
    staleTime: 30_000,
  })
  const stats = useQuery({
    queryKey: [...STATS_QUERY_KEY, days],
    queryFn: () => getStats(days),
    staleTime: 60_000,
  })

  const accountByKey = useMemo(() => {
    const map = new Map<string, CpaAccount>()
    for (const account of overview.data?.accounts ?? []) {
      const authIndex = account.credential?.auth_index
      if (typeof authIndex === 'string' && authIndex) {
        map.set(`${account.instance_id}/${authIndex}`, account)
      }
    }
    return map
  }, [overview.data])

  const rows = useMemo<Row[]>(() => {
    const list = (stats.data?.accounts ?? []).map((stat) => {
      const key = `${stat.instance_id}/${stat.auth_index}`
      const account = accountByKey.get(key)
      return {
        ...stat,
        key,
        label: accountDisplayName(stat),
        socks: account ? socksStateOf(account) : null,
      }
    })
    const dir = sort.desc ? -1 : 1
    return list.sort((a, b) => {
      if (sort.key === 'name') return a.label.localeCompare(b.label) * dir
      const av = (a[sort.key] as number | null | undefined) ?? -1
      const bv = (b[sort.key] as number | null | undefined) ?? -1
      return (av - bv) * dir
    })
  }, [stats.data, accountByKey, sort])

  const cost = useMemo(() => buildCostSummary(stats.data), [stats.data])

  const trendSpec = useMemo(() => {
    const points = stats.data?.quota_points ?? []
    const names = new Map<string, string>()
    for (const account of overview.data?.accounts ?? []) {
      names.set(
        `${account.instance_id}/${account.name}`,
        accountDisplayName(account)
      )
    }
    const values = points
      .filter((p) => p.seven_day != null)
      .sort((a, b) => a.ts - b.ts)
      .map((p) => ({
        time: p.ts * 1000,
        account: names.get(`${p.instance_id}/${p.name}`) ?? p.name,
        remaining: Math.round((100 - (p.seven_day ?? 0)) * 10) / 10,
      }))
    return {
      type: 'line',
      data: [{ id: 'quota', values }],
      xField: 'time',
      yField: 'remaining',
      seriesField: 'account',
      point: { visible: false },
      line: { style: { lineWidth: 2 } },
      legends: { visible: true, orient: 'bottom' },
      axes: [
        {
          orient: 'bottom',
          type: 'time',
          layers: [{ timeFormat: '%m-%d %H:%M' }],
        },
        { orient: 'left', type: 'linear', min: 0, max: 100 },
      ],
      tooltip: {
        dimension: {
          content: [
            {
              key: (d: { account: string }) => d.account,
              value: (d: { remaining: number }) => `${d.remaining}%`,
            },
          ],
        },
      },
      background: 'transparent',
      theme: resolvedTheme === 'dark' ? 'dark' : 'light',
    }
  }, [stats.data, overview.data, resolvedTheme])

  const toggleSort = (key: SortKey) =>
    setSort((prev) =>
      prev.key === key ? { key, desc: !prev.desc } : { key, desc: true }
    )

  const header = (key: SortKey, label: string, align = 'text-right') => (
    <TableHead className={align}>
      <button
        type='button'
        className='hover:text-foreground inline-flex items-center gap-1'
        onClick={() => toggleSort(key)}
      >
        {label}
        <ArrowDownUp
          className={cn(
            'size-3',
            sort.key === key ? 'opacity-100' : 'opacity-30'
          )}
        />
      </button>
    </TableHead>
  )

  const loading = stats.isLoading || overview.isLoading
  const collecting = (overview.data?.instances ?? []).some(
    (i) => i.usage_collect_enabled
  )
  const totalCost = cost.planCostUSD + cost.proxyCostUSD
  const hasPoints = (stats.data?.quota_points.length ?? 0) > 0
  let paybackTone: 'good' | 'bad' | undefined
  if (cost.payback !== null) {
    paybackTone = cost.payback >= 1 ? 'good' : 'bad'
  }

  return (
    <div className='space-y-3 sm:space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Tabs value={String(days)} onValueChange={(v) => setDays(Number(v))}>
          <TabsList>
            {RANGE_OPTIONS.map((d) => (
              <TabsTrigger key={d} value={String(d)} className='px-2.5 text-xs'>
                {t('{{n}} days', { n: d })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        {!collecting && !loading && (
          <span className='text-xs text-amber-600 dark:text-amber-400'>
            {t(
              'Usage collection is off on every CPA, so request, token and cost columns stay empty'
            )}
          </span>
        )}
      </div>

      {/* ③ comparison table */}
      <Section
        icon={<Table2 />}
        title={t('Account comparison')}
        description={t('Click a column header to sort')}
      >
        {loading ? (
          <Skeleton className='m-3 h-40' />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                {header('name', t('Account'), 'text-left')}
                <TableHead>SOCKS</TableHead>
                {header('requests', t('Requests success / failed'))}
                {header('tokens', t('Tokens'))}
                {header('revenue_usd', t('New API revenue (est.)'))}
                {header('cost_usd', t('Official API price'))}
                {header('five_hour', t('5h / 7d left'))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className='text-muted-foreground text-center text-xs'
                  >
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
              {rows.map((row) => {
                const left5 = remaining(row.five_hour)
                const left7 = remaining(row.seven_day)
                return (
                  <TableRow key={row.key}>
                    <TableCell className='max-w-56 truncate'>
                      {row.label}
                      {row.plan_type && PLAN_LABEL_KEYS[row.plan_type]
                        ? ` · ${t(PLAN_LABEL_KEYS[row.plan_type])}`
                        : ''}
                    </TableCell>
                    <TableCell
                      className={cn(
                        'text-xs',
                        row.socks && SOCKS_CLASS[row.socks]
                      )}
                    >
                      {row.socks ? t(SOCKS_LABEL[row.socks]) : '-'}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatCompactNumber(row.requests - row.failed, locale)}
                      {' / '}
                      <span className='text-red-600 dark:text-red-400'>
                        {formatCompactNumber(row.failed, locale)}
                      </span>
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatCompactNumber(row.tokens, locale)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatCurrencyFromUSD(row.revenue_usd, {
                        compact: true,
                      })}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {usd(row.cost_usd, locale)}
                      {row.unpriced_requests > 0 && (
                        <span
                          className='text-muted-foreground ml-1 text-xs'
                          title={t(
                            '{{n}} requests have no price and are not counted',
                            {
                              n: row.unpriced_requests,
                            }
                          )}
                        >
                          *
                        </span>
                      )}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {row.disabled
                        ? t('Disabled')
                        : `${left5 === null ? '-' : `${Math.round(left5)}%`} / ${
                            left7 === null ? '-' : `${Math.round(left7)}%`
                          }`}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </Section>

      {/* ④ quota trend */}
      <Section
        icon={<LineChart />}
        title={t('7-day quota left trend')}
        description={t(
          'One point per refresh, at most every 10 minutes, kept for 14 days'
        )}
      >
        <div className='h-[300px] p-1.5 sm:h-80 sm:p-2'>
          {loading && <Skeleton className='h-full w-full' />}
          {!loading && !hasPoints && (
            <div className='text-muted-foreground flex h-full items-center justify-center text-xs'>
              {t('No quota points yet. They appear after the next refresh.')}
            </div>
          )}
          {!loading && hasPoints && themeReady && (
            <VChart
              key={`cpa-quota-${days}-${resolvedTheme}`}
              spec={trendSpec}
              option={VCHART_OPTION}
            />
          )}
        </div>
      </Section>

      {/* ⑤ cost vs revenue */}
      <Section
        icon={<Coins />}
        title={t('Cost vs revenue ({{n}} days, estimated)', { n: days })}
        description={t(
          'Revenue is what New API charged on the CPA channels, split by token share. Plan cost uses official prices.'
        )}
        actions={
          <ProxyCostEditor
            value={overview.data?.settings.proxy_monthly_cost_usd ?? 0}
          />
        }
      >
        <div className='grid grid-cols-2 gap-2 p-3 sm:p-4 lg:grid-cols-5'>
          <CostMetric
            label={t('Plan cost')}
            value={usd(cost.planCostUSD, locale)}
            hint={
              cost.unknownPlanAccounts > 0
                ? t('{{n}} account(s) without official price', {
                    n: cost.unknownPlanAccounts,
                  })
                : undefined
            }
          />
          <CostMetric
            label={t('Proxy cost')}
            value={usd(cost.proxyCostUSD, locale)}
          />
          <CostMetric
            label={t('New API revenue')}
            value={formatCurrencyFromUSD(cost.revenueUSD, { compact: true })}
          />
          <CostMetric
            label={t('At official API price')}
            value={usd(cost.officialCostUSD, locale)}
          />
          <CostMetric
            label={t('Payback')}
            value={cost.payback === null ? '-' : `${cost.payback.toFixed(1)}×`}
            hint={
              totalCost > 0
                ? t('revenue ÷ (plan + proxy)')
                : t('Set the proxy cost to see payback')
            }
            tone={paybackTone}
          />
        </div>
      </Section>

      {/* ⑥ weekly cycle comparison (design 2026-11-11) */}
      <CycleSection />
    </div>
  )
}
