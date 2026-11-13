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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { accountDisplayName } from '../lib/pool'
import {
  BLOCK_LABEL_KEYS,
  REASON_LABEL_KEYS,
  perAccountRate,
  previewGoing,
  rateText,
  rotationKey,
} from '../lib/rotation'
import type { CpaAccount, CpaRotationState, CpaRotationSummary } from '../types'

/**
 * What switching rotation on would do right now (safety gate 5, user
 * decision 2026-11-12): who goes offline in the first step and why, who
 * waits and what holds them back, and the load of each CPA before and
 * after. Nothing here has happened yet.
 */
export function RotationPreview(props: {
  preview: CpaRotationSummary
  current: CpaRotationSummary | undefined
  accounts: CpaAccount[]
  instanceNames: Map<number, string>
  dryRun: boolean
}) {
  const { t } = useTranslation()
  const names = new Map(
    props.accounts.map((account) => [
      rotationKey(account),
      accountDisplayName(account),
    ])
  )
  const label = (row: CpaRotationState) =>
    names.get(rotationKey(row)) ?? row.name
  const cpa = (id: number) => props.instanceNames.get(id) ?? `#${id}`
  const going = previewGoing(props.preview, props.current)
  const queued = props.preview.accounts.filter((row) => row.state === 'queued')
  const pools = props.preview.pools ?? []
  const perAccount = (peak: number, online: number) => {
    const value = perAccountRate(peak, online)
    return value === null ? '-' : rateText(value)
  }

  return (
    <div className='space-y-4 text-sm'>
      {props.dryRun && (
        <div className='rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:bg-amber-500/15 dark:text-amber-200'>
          {t(
            'Dry run is on: after saving nobody is taken offline, the page only shows who would be.'
          )}
        </div>
      )}

      <div>
        <div className='font-semibold'>
          {t('Goes offline in the first step')}
          <span className='text-muted-foreground ml-1.5 font-normal tabular-nums'>
            {going.length}
          </span>
        </div>
        {going.length === 0 ? (
          <div className='text-muted-foreground mt-1 text-xs'>
            {t('Nobody goes offline right after switching on.')}
          </div>
        ) : (
          <ul className='mt-1 divide-y rounded-md border'>
            {going.map((row) => (
              <li
                key={rotationKey(row)}
                className='flex items-center gap-2 px-3 py-1.5 text-xs'
              >
                <span className='min-w-0 flex-1 truncate font-medium'>
                  {label(row)}
                </span>
                <span className='text-muted-foreground flex-none'>
                  {cpa(row.instance_id)}
                </span>
                <span className='flex-none text-sky-700 dark:text-sky-300'>
                  {t(REASON_LABEL_KEYS[row.reason ?? 'rotation'])}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {queued.length > 0 && (
        <div>
          <div className='font-semibold'>
            {t('Waiting, still online')}
            <span className='text-muted-foreground ml-1.5 font-normal tabular-nums'>
              {queued.length}
            </span>
          </div>
          <ul className='mt-1 divide-y rounded-md border'>
            {queued.map((row) => (
              <li
                key={rotationKey(row)}
                className='flex flex-wrap items-center gap-x-2 gap-y-0.5 px-3 py-1.5 text-xs'
              >
                <span className='min-w-0 flex-1 truncate font-medium'>
                  {label(row)}
                </span>
                <span className='text-muted-foreground flex-none'>
                  {cpa(row.instance_id)}
                </span>
                <span className='flex-none'>
                  {t(REASON_LABEL_KEYS[row.reason ?? 'rotation'])}
                </span>
                {row.blocked && (
                  <span className='text-muted-foreground w-full'>
                    {t('Held back: {{why}}', {
                      why: t(BLOCK_LABEL_KEYS[row.blocked]),
                    })}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {pools.length > 0 && (
        <div>
          <div className='font-semibold'>{t('Load of each CPA')}</div>
          <div className='mt-1 rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>CPA</TableHead>
                  <TableHead className='text-right'>
                    {t('Online, now → after')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('Peak RPM, last hour (no 429)')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('RPM per account, now → after')}
                  </TableHead>
                  <TableHead className='text-right'>
                    {t('Recent 429s')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pools.map((pool) => (
                  <TableRow key={pool.instance_id}>
                    <TableCell>{cpa(pool.instance_id)}</TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {pool.online_before} → {pool.online}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {rateText(pool.peak_rpm)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {perAccount(pool.peak_rpm, pool.online_before)} →{' '}
                      {perAccount(pool.peak_rpm, pool.online)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {pool.recent_429}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      )}

      <p className='text-muted-foreground text-xs'>
        {t(
          'Working hours count from the moment rotation is switched on, so nobody rests for work done before. Later accounts follow one step at a time and every step is checked again.'
        )}
      </p>
    </div>
  )
}
