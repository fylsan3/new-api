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
import { VChart } from '@visactor/react-vchart'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useTheme } from '@/context/theme-provider'
import { VCHART_OPTION } from '@/lib/vchart'

import { RATE_SERIES_QUERY_KEY, getRateSeries, type AccountRef } from '../api'
import {
  buildRateRows,
  type LineDatum,
  type RateMetric,
  type StackDatum,
} from '../lib/rate'
import { rateText } from '../lib/rotation'
import type { RateRange } from '../types'

const RANGES: RateRange[] = ['1h', '12h', '24h', '7d']

const RANGE_LABEL_KEYS: Record<RateRange, string> = {
  '1h': 'Last 1 hour',
  '12h': 'Last 12 hours',
  '24h': 'Last 1 day',
  '7d': 'Last 7 days',
}

/** Loads the VChart theme once per page theme (same as the stats page). */
function useVChartTheme() {
  const { resolvedTheme } = useTheme()
  const [ready, setReady] = useState(false)
  useEffect(() => {
    let cancelled = false
    setReady(false)
    void import('@visactor/vchart').then((m) => {
      if (cancelled) return
      m.ThemeManager.setCurrentTheme(
        resolvedTheme === 'dark' ? 'dark' : 'light'
      )
      setReady(true)
    })
    return () => {
      cancelled = true
    }
  }, [resolvedTheme])
  return { ready, theme: resolvedTheme === 'dark' ? 'dark' : 'light' }
}

const BAR_COLOR: Record<RateMetric, string> = { rpm: '#D97757', tpm: '#3B82F6' }
const LINE_COLOR: Record<RateMetric, string> = {
  rpm: '#9A3412',
  tpm: '#1E40AF',
}
const FAIL_COLOR = '#DC2626'

/**
 * RPM / TPM chart of one account (accountRef) or the whole pool. Range and
 * metric are switchable. 1 h is an area with one point per minute. 12 h
 * (10-minute periods), 1 day (30 minutes) and 7 days (2 hours) show the
 * busiest minute of each period as a bar and the per-minute average as a
 * line, so wide periods do not hide spikes. Requests are split into
 * succeeded and failed; 429s are left out and listed in the tooltip.
 */
export function RateChart(props: { accountRef?: AccountRef; height: number }) {
  const { t } = useTranslation()
  const [range, setRange] = useState<RateRange>('1h')
  const [metric, setMetric] = useState<RateMetric>('rpm')
  const { ready, theme } = useVChartTheme()

  const query = useQuery({
    queryKey: [
      ...RATE_SERIES_QUERY_KEY,
      range,
      props.accountRef?.instance_id ?? 0,
      props.accountRef?.name ?? '',
    ],
    queryFn: () => getRateSeries(range, props.accountRef),
    staleTime: 30_000,
    refetchInterval: 60_000,
  })

  const points = query.data?.points
  const step = query.data?.step ?? 60
  // One point is already one minute in the 1 h range: no separate average.
  const wide = range !== '1h'
  const split = metric === 'rpm'
  const averageLabel = t('Per-minute average')
  const peakLabel = t('Peak of the period')
  const okLabel = t('Succeeded')
  const failLabel = t('Failed')
  const spec = useMemo(() => {
    const format = range === '1h' ? 'HH:mm' : 'MM-DD HH:mm'
    const rows = buildRateRows(points ?? [], metric, step, format)
    const partLabel = (d: StackDatum) => {
      if (!split) return wide ? peakLabel : metric.toUpperCase()
      if (d.part === 'fail') return failLabel
      return wide ? `${peakLabel} · ${okLabel}` : okLabel
    }
    const fill = (d: StackDatum) =>
      d.part === 'fail' ? FAIL_COLOR : BAR_COLOR[metric]
    const stackContent = [
      { key: partLabel, value: (d: StackDatum) => rateText(d.value) },
      {
        key: '429',
        value: (d: StackDatum) => String(d.r429),
        // Once per period: only on the first row of the stack, and not
        // when the average line already lists it.
        visible: (d: StackDatum) => !wide && d.part === 'ok',
      },
    ]
    const stack = {
      type: wide ? 'bar' : 'area',
      dataIndex: 0,
      xField: 'time',
      yField: 'value',
      seriesField: 'part',
      stack: true,
      bar: { style: { fill } },
      area: { style: { fill, fillOpacity: 0.35, curveType: 'monotone' } },
      line: { style: { stroke: fill, lineWidth: 1.5, curveType: 'monotone' } },
      point: { visible: false },
      tooltip: {
        mark: { content: stackContent },
        dimension: { content: stackContent },
      },
    }
    const lineContent = [
      { key: averageLabel, value: (d: LineDatum) => rateText(d.avg) },
      { key: '429', value: (d: LineDatum) => String(d.r429) },
    ]
    const average = {
      type: 'line',
      dataIndex: 1,
      xField: 'time',
      yField: 'avg',
      line: { style: { stroke: LINE_COLOR[metric], lineWidth: 1.5 } },
      point: { visible: false },
      tooltip: {
        mark: { content: lineContent },
        dimension: { content: lineContent },
      },
    }
    return {
      type: 'common',
      data: [
        { id: 'stack', values: rows.stack },
        { id: 'line', values: rows.line },
      ],
      series: wide ? [stack, average] : [stack],
      axes: [
        {
          orient: 'bottom',
          type: 'band',
          label: { autoHide: true },
          tick: { visible: false },
        },
        {
          orient: 'left',
          type: 'linear',
          label: {
            formatMethod: (v: number) => rateText(v),
          },
        },
      ],
      legends: { visible: false },
      padding: 8,
      background: 'transparent',
      theme,
    }
  }, [
    points,
    step,
    metric,
    range,
    wide,
    split,
    averageLabel,
    peakLabel,
    okLabel,
    failLabel,
    theme,
  ])

  const empty = (points ?? []).every((p) => p.requests === 0)

  return (
    <div className='space-y-1.5'>
      <div className='flex flex-wrap items-center justify-between gap-1.5'>
        <Tabs value={range} onValueChange={(v) => setRange(v as RateRange)}>
          <TabsList className='h-7'>
            {RANGES.map((r) => (
              <TabsTrigger key={r} value={r} className='px-2 text-[11px]'>
                {t(RANGE_LABEL_KEYS[r])}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Tabs value={metric} onValueChange={(v) => setMetric(v as RateMetric)}>
          <TabsList className='h-7'>
            <TabsTrigger value='rpm' className='px-2 text-[11px]'>
              RPM
            </TabsTrigger>
            <TabsTrigger value='tpm' className='px-2 text-[11px]'>
              TPM
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      <div className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-0.5 text-[11px]'>
        <span className='flex items-center gap-1.5'>
          <span
            className='inline-block size-2 rounded-[2px]'
            style={{ background: BAR_COLOR[metric] }}
          />
          {wide && peakLabel}
          {wide && split && ' · '}
          {split && okLabel}
          {!wide && !split && t('Per minute')}
        </span>
        {split && (
          <span className='flex items-center gap-1.5'>
            <span
              className='inline-block size-2 rounded-[2px]'
              style={{ background: FAIL_COLOR }}
            />
            {failLabel}
          </span>
        )}
        {wide && (
          <span className='flex items-center gap-1.5'>
            <span
              className='inline-block h-0.5 w-3'
              style={{ background: LINE_COLOR[metric] }}
            />
            {averageLabel}
          </span>
        )}
        {split && <span>{t('429s not counted')}</span>}
      </div>
      <div style={{ height: props.height }}>
        {(query.isLoading || !ready) && <Skeleton className='h-full w-full' />}
        {!query.isLoading && ready && empty && (
          <div className='text-muted-foreground flex h-full items-center justify-center text-xs'>
            {t('No requests in this range')}
          </div>
        )}
        {!query.isLoading && ready && !empty && (
          <VChart
            key={`rate-${range}-${metric}-${theme}`}
            spec={spec}
            option={VCHART_OPTION}
          />
        )}
      </div>
    </div>
  )
}
