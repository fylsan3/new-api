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

import { toIntlLocale } from '@/i18n/languages'
import { formatCurrencyFromUSD } from '@/lib/currency'
import {
  formatCompactNumber,
  formatNumber,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'

import { credentialCount, lastRequestAt } from '../lib/card'
import {
  buildDollarWindows,
  buildExtraUsage,
  buildQuotaWindows,
} from '../lib/quota'
import type { CpaAccount, CpaAccountStats, CpaCardUsage } from '../types'

export type AccountDetailsProps = {
  account: CpaAccount
  instanceName: string
  usage: CpaCardUsage | null
  /** Per-account totals from GET /stats (30 days); null while loading. */
  stats: CpaAccountStats | null
  collecting: boolean
}

function formatIso(value: string | null | undefined) {
  if (!value) return '-'
  const ms = Date.parse(value)
  if (Number.isNaN(ms)) return '-'
  return formatTimestampToDate(ms, 'milliseconds')
}

function formatMoney(value: number | null, currency = 'USD') {
  if (value === null) return '-'
  return `${currency === 'USD' ? '$' : `${currency} `}${value.toFixed(2)}`
}

function text(value: unknown) {
  return typeof value === 'string' && value.trim() !== '' ? value : null
}

/**
 * Body of the hover popover shown next to an account card. Everything that
 * does not need to be visible at a glance lives here.
 */
export function AccountDetails(props: AccountDetailsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const account = props.account
  const cred = account.credential ?? {}
  const success = credentialCount(cred.success)
  const failed = credentialCount(cred.failed)
  const last = lastRequestAt(account)
  const fromQueue = (account.last_request_at ?? 0) > 0
  const windows = buildQuotaWindows(account.usage)
  const dollarWindows = buildDollarWindows(account.usage)
  const extra = buildExtraUsage(account.usage)
  const stats = props.stats
  const status = [text(cred.status), text(cred.status_message)]
    .filter(Boolean)
    .join(' · ')
  const tokens = (value: number | undefined) =>
    value === undefined ? '-' : formatCompactNumber(value, locale)

  const others: Array<[string, string]> = []
  if (extra && extra.isEnabled) {
    others.push([
      t('Extra usage'),
      `${formatMoney(
        extra.usedCredits === null ? null : extra.usedCredits / 100,
        extra.currency ?? 'USD'
      )} / ${formatMoney(
        extra.monthlyLimit === null ? null : extra.monthlyLimit / 100,
        extra.currency ?? 'USD'
      )}`,
    ])
  }
  for (const balance of dollarWindows) {
    others.push([
      `${t('Balance')} (${balance.key})`,
      `${formatMoney(balance.remainingDollars)} / ${formatMoney(balance.limitDollars)}`,
    ])
  }
  if (account.quota_observed_at) {
    others.push([
      t('Quota checked at'),
      formatTimestampToDate(account.quota_observed_at),
    ])
  }
  if (account.quota_error) {
    others.push([t('Quota check error'), account.quota_error])
  }
  if (account.reset_grants_error) {
    others.push([t('Reset grant check error'), account.reset_grants_error])
  }

  return (
    <div className='space-y-2.5 text-xs'>
      <Section title={t('Account details')}>
        <Row label={t('File name')} value={account.name} />
        <Row label={t('Source CPA')} value={props.instanceName} />
        {status && <Row label={t('Status')} value={status} />}
        <Row
          label={t('Proxy')}
          value={account.proxy_url || t('Not configured')}
          mono
        />
        <Row
          label={t('Last token refresh')}
          value={formatIso(text(cred.last_refresh))}
        />
      </Section>

      <Section title={t('Request stats')}>
        <Row
          label={t('Last request')}
          value={
            last
              ? `${formatTimestampToDate(last)} (${formatTimestampRelative(last, 'seconds', locale)}${fromQueue ? '' : `, ${t('about, 10-minute buckets')}`})`
              : '-'
          }
        />
        <Row
          label={t('Success / failed (since CPA start)')}
          value={`${formatNumber(success)} / ${formatNumber(failed)}`}
        />
      </Section>

      <Section title={t('Usage (30 days, New API collected)')}>
        {props.collecting ? (
          <>
            <Row
              label={t('Requests / failed')}
              value={
                stats
                  ? `${formatNumber(stats.requests)} / ${formatNumber(stats.failed)}`
                  : formatNumber(props.usage?.requests)
              }
            />
            <Row
              label={t('Input tokens')}
              value={tokens(stats?.input_tokens)}
            />
            <Row
              label={t('Output tokens')}
              value={tokens(stats?.output_tokens)}
            />
            <Row
              label={t('Cache read / write')}
              value={`${tokens(stats?.cache_read_tokens)} / ${tokens(stats?.cache_write_tokens)}`}
            />
            <Row
              label={t('Spend (30 days, base price)')}
              value={formatCurrencyFromUSD(
                stats?.cost_usd ?? props.usage?.cost_usd ?? null
              )}
            />
            <p className='text-muted-foreground'>
              {t(
                'Spend is estimated from New API model prices without group ratios. It is not billed.'
              )}
            </p>
          </>
        ) : (
          <p className='text-muted-foreground'>
            {t(
              'Spend needs "Collect usage" turned on for this CPA. Success/failed counts reset when CPA restarts.'
            )}
          </p>
        )}
      </Section>

      {windows.length > 0 && (
        <Section title={t('Quota reset times')}>
          {windows.map((window) => (
            <Row
              key={window.id}
              label={t(window.labelKey)}
              value={formatIso(window.resetsAt)}
            />
          ))}
        </Section>
      )}

      {others.length > 0 && (
        <Section title={t('Other')}>
          {others.map(([label, value]) => (
            <Row key={label} label={label} value={value} />
          ))}
        </Section>
      )}
    </div>
  )
}

function Section(props: { title: string; children: React.ReactNode }) {
  return (
    <div className='space-y-1'>
      <div className='text-foreground font-medium'>{props.title}</div>
      {props.children}
    </div>
  )
}

function Row(props: { label: string; value: string; mono?: boolean }) {
  return (
    <div className='flex justify-between gap-3'>
      <span className='text-muted-foreground shrink-0'>{props.label}</span>
      <span
        className={
          props.mono ? 'text-right font-mono break-all' : 'text-right break-all'
        }
      >
        {props.value}
      </span>
    </div>
  )
}
