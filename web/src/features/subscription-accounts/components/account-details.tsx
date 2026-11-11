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
import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { copyToClipboard } from '@/lib/copy-to-clipboard'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import { toIntlLocale } from '@/i18n/languages'
import { formatCurrencyFromUSD } from '@/lib/currency'
import {
  formatCompactNumber,
  formatNumber,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'

import {
  FAIL_CATEGORIES,
  FAIL_CATEGORY_KEYS,
  credentialCount,
  groupFailReasons,
  lastRequestAt,
  quotaErrorKey,
} from '../lib/card'
import {
  buildDollarWindows,
  buildEffectiveQuota,
  buildExtraUsage,
  buildPassiveQuota,
  buildResetGrants,
  nextAutoRefreshAt,
} from '../lib/quota'
import type {
  CpaAccount,
  CpaAccountStats,
  CpaCardUsage,
  ResetGrantInfo,
} from '../types'

export type AccountDetailsProps = {
  account: CpaAccount
  instanceName: string
  usage: CpaCardUsage | null
  /** Per-account totals from GET /stats (30 days); null while loading. */
  stats: CpaAccountStats | null
  collecting: boolean
}

/** Optimization A: quota data older than this is flagged amber. */
const QUOTA_STALE_SECONDS = 3600

/** Dates in the popover drop the year when it is the current year. */
function formatShort(ms: number) {
  const d = dayjs(ms)
  return d.format(d.year() === dayjs().year() ? 'MM-DD HH:mm:ss' : 'YYYY-MM-DD HH:mm:ss')
}

function formatIso(value: string | null | undefined) {
  if (!value) return '-'
  const ms = Date.parse(value)
  if (Number.isNaN(ms)) return '-'
  return formatShort(ms)
}

/** Time only when it is today, otherwise short date. */
function formatClock(seconds: number) {
  const d = dayjs(seconds * 1000)
  return d.isSame(dayjs(), 'day') ? d.format('HH:mm:ss') : formatShort(seconds * 1000)
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
  const windows = buildEffectiveQuota(account, Date.now()).windows
  const passive = buildPassiveQuota(account.credential)
  const nextAuto = nextAutoRefreshAt(account)
  const dollarWindows = buildDollarWindows(account.usage)
  const extra = buildExtraUsage(account.usage)
  const stats = props.stats
  const resetGrants = account.is_claude
    ? buildResetGrants(account.reset_grants)
    : null
  const failGroups = groupFailReasons(props.usage?.fail_reasons_7d)
  const failed7d = props.usage?.failed_7d ?? 0
  const lastFail = props.usage?.last_fail
  const status = [text(cred.status), text(cred.status_message)]
    .filter(Boolean)
    .join(' · ')
  const tokens = (value: number | undefined) =>
    value === undefined ? '-' : formatCompactNumber(value, locale)

  const others: Array<[string, string, string?]> = []
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
    const age = Math.floor(Date.now() / 1000) - account.quota_observed_at
    const old = age > QUOTA_STALE_SECONDS
    others.push([
      t('Quota checked at'),
      old
        ? `${formatShort(account.quota_observed_at * 1000)} (${t('over 1 hour ago')})`
        : formatShort(account.quota_observed_at * 1000),
      old ? 'text-amber-700 dark:text-amber-300' : undefined,
    ])
  }
  if (passive) {
    others.push([
      t('Chat quota updated at'),
      formatShort(passive.observedAt * 1000),
    ])
  }
  if (account.is_claude && account.auto_refresh_day) {
    others.push([
      t('Next auto refresh'),
      nextAuto ? formatClock(nextAuto) : t('Done for today'),
    ])
  }
  if (account.auto_refresh_error) {
    others.push([
      t('Last auto refresh'),
      `${account.auto_refresh_last_at ? formatClock(account.auto_refresh_last_at) : '-'} · ${t('skipped or failed')}`,
      'text-amber-700 dark:text-amber-300',
    ])
  }
  const errorText = (error: string) => {
    const key = quotaErrorKey(error)
    return key ? t(key) : error
  }
  const errors: string[] = []
  if (account.quota_error) errors.push(errorText(account.quota_error))

  return (
    <div className='text-xs leading-normal'>
      <div className='text-[13px] font-medium'>{t('Account details')}</div>
      <div className='space-y-1'>
        <div>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span>{t('File name')}</span>
            <button
              type='button'
              className='hover:text-foreground inline-flex cursor-pointer items-center gap-1 text-[11px]'
              onClick={async () => {
                if (await copyToClipboard(account.name)) toast.success(t('Copied'))
              }}
            >
              <Copy className='size-3' />
              {t('Copy')}
            </button>
          </div>
          <div className='text-muted-foreground font-mono text-[11px] break-all'>
            {account.name}
          </div>
        </div>
        <Row label={t('Source CPA')} value={props.instanceName} />
        {status && <Row label={t('Status')} value={status} />}
        {account.proxy_url ? (
          <Row label={t('Proxy')} value={account.proxy_url} mono />
        ) : (
          <Row
            label={t('Proxy')}
            value={t('Not configured')}
            valueClassName='text-amber-700 dark:text-amber-300'
          />
        )}
        <Row
          label={t('Last token refresh')}
          value={formatIso(text(cred.last_refresh))}
        />
      </div>

      <Section title={t('Request stats (since CPA start)')}>
        <Row
          label={t('Last request')}
          value={
            last
              ? `${formatClock(last)} (${formatTimestampRelative(last, 'seconds', locale)})`
              : '-'
          }
          title={
            last
              ? `${formatTimestampToDate(last)}${fromQueue ? '' : ` · ${t('about, 10-minute buckets')}`}`
              : undefined
          }
        />
        <div className='flex justify-between gap-3'>
          <span className='text-muted-foreground shrink-0'>
            {t('Success / failed')}
          </span>
          <span className='text-right'>
            {formatNumber(success)} /{' '}
            <span className={cn(failed > 0 && 'text-red-600 dark:text-red-400')}>
              {formatNumber(failed)}
            </span>
          </span>
        </div>
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
            {t('"Collect usage" is off for this CPA, no spend data yet.')}
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

      {account.is_claude &&
        (account.reset_grants_error ||
          (resetGrants && resetGrants.grants.length > 0)) && (
          <Section
            title={
              account.quota_checked_at
                ? `${t('Reset grants')} (${t('fetched on refresh at {{time}}', {
                    time: formatClock(account.quota_checked_at),
                  })})`
                : t('Reset grants')
            }
          >
            {account.reset_grants_error ? (
              <div className='rounded-md bg-amber-50 px-2 py-1.5 leading-normal break-all text-amber-900 dark:bg-amber-500/15 dark:text-amber-200'>
                {`${t('Reset grant check error')}: ${errorText(account.reset_grants_error)}`}
              </div>
            ) : (
              resetGrants?.grants.map((grant, i) => {
                const stateKey = grantStateKey(grant)
                const state = stateKey ? t(stateKey) : null
                return (
                  <Row
                    key={grant.id}
                    label={t('Grant {{n}} · {{left}} of {{total}} left', {
                      n: i + 1,
                      left: grant.resetsLeft,
                      total: grant.resetsTotal,
                    })}
                    value={[
                      grant.endsAt
                        ? t('Ends {{time}}', { time: formatIso(grant.endsAt) })
                        : null,
                      state,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                    title={grant.label || undefined}
                    valueClassName={cn(
                      grant.paused === true &&
                        'text-amber-700 dark:text-amber-300'
                    )}
                  />
                )
              })
            )}
          </Section>
        )}

      {props.collecting && failed7d > 0 && (
        <Section title={t('Failure reasons, last 7 days')}>
          {FAIL_CATEGORIES.filter((c) => failGroups[c] > 0).map((category) => (
            <Row
              key={category}
              label={t(FAIL_CATEGORY_KEYS[category])}
              value={formatNumber(failGroups[category])}
            />
          ))}
          {lastFail && (
            <p className='text-muted-foreground'>
              {t('Latest')}: {formatShort(lastFail.at * 1000)} ·{' '}
              {lastFail.status_code || '-'}
              {lastFail.code ? ` · ${lastFail.code}` : ''}
            </p>
          )}
        </Section>
      )}

      {(others.length > 0 || errors.length > 0) && (
        <Section title={t('Quota check')}>
          {others.map(([label, value, className]) => (
            <Row
              key={label}
              label={label}
              value={value}
              valueClassName={className}
            />
          ))}
          {errors.map((error) => (
            <div
              key={error}
              className='rounded-md bg-amber-50 px-2 py-1.5 leading-normal break-all text-amber-900 dark:bg-amber-500/15 dark:text-amber-200'
            >
              {error}
            </div>
          ))}
        </Section>
      )}
    </div>
  )
}

/** Translation key of a reset grant's state, null when not reported. */
function grantStateKey(grant: ResetGrantInfo): string | null {
  if (grant.paused === true) return 'Paused'
  if (grant.usableNow === true) return 'Usable'
  if (grant.usableNow === false) return 'Not usable now'
  return null
}

/** Small grey title with a divider above (agreed design v7). */
function Section(props: { title: string; children: React.ReactNode }) {
  return (
    <div className='mt-2.5 space-y-1 border-t pt-2'>
      <div className='text-muted-foreground text-[11px]'>{props.title}</div>
      {props.children}
    </div>
  )
}

function Row(props: {
  label: string
  value: string
  mono?: boolean
  title?: string
  valueClassName?: string
}) {
  return (
    <div className='flex justify-between gap-3' title={props.title}>
      <span className='text-muted-foreground shrink-0'>{props.label}</span>
      <span
        className={cn(
          'min-w-0 text-right break-all',
          props.mono && 'font-mono text-[11px]',
          props.valueClassName
        )}
      >
        {props.value}
      </span>
    </div>
  )
}
