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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  OVERVIEW_QUERY_KEY,
  claimResetGrant,
  refreshAccount,
  refreshAccountToken,
  setAccountDisabled,
} from '../api'
import { socksStateOf } from '../lib/card'
import {
  PLAN_LABEL_KEYS,
  buildDollarWindows,
  buildExtraUsage,
  buildQuotaWindows,
  buildResetGrants,
  quotaLevel,
  remainingPercent,
} from '../lib/quota'
import type { CpaAccount, CpaAlert, CpaCardUsage } from '../types'
import { ModelsDialog } from './models-dialog'
import { SocksBadge } from './socks-badge'
import { StatsLine } from './stats-line'

type PendingAction = 'enable' | 'disable' | 'token' | 'reset' | null

const LEVEL_BAR_CLASS = {
  ok: 'bg-emerald-500',
  warn: 'bg-amber-500',
  danger: 'bg-red-500',
  unknown: 'bg-muted-foreground/40',
} as const

const LEVEL_TEXT_CLASS = {
  ok: 'text-emerald-700 dark:text-emerald-300',
  warn: 'text-amber-700 dark:text-amber-300',
  danger: 'text-red-600 dark:text-red-400',
  unknown: 'text-muted-foreground',
} as const

const RED_OUTLINE =
  'border-red-500 text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-500/10'

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

function credentialText(value: unknown) {
  if (typeof value === 'string' && value.trim() !== '') return value
  if (typeof value === 'number') return String(value)
  return null
}

type AccountCardProps = {
  account: CpaAccount
  instanceName: string
  usage: CpaCardUsage | null
  collecting: boolean
  alerts: CpaAlert[]
}

export function AccountCard(props: AccountCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<PendingAction>(null)
  const [modelsOpen, setModelsOpen] = useState(false)

  const account = props.account
  const cred = account.credential ?? {}
  const ref = { instance_id: account.instance_id, name: account.name }
  const disabled = cred.disabled === true
  const socks = socksStateOf(account)
  // Anthropic calls are refused without a working SOCKS proxy.
  const proxyReady = socks === 'ok'

  const windows = buildQuotaWindows(account.usage)
  const dollarWindows = buildDollarWindows(account.usage)
  const extra = buildExtraUsage(account.usage)
  const grants = buildResetGrants(account.reset_grants)
  const resetsLeft = grants?.resetsLeft ?? 0
  const hasResets = resetsLeft > 0
  const activeGrants = (grants?.grants ?? []).filter((g) => g.resetsLeft > 0)
  const resetsTotal = activeGrants.reduce((sum, g) => sum + g.resetsTotal, 0)
  const nextEnds = activeGrants
    .map((g) => g.endsAt)
    .filter((v): v is string => !!v)
    .sort()[0]
  const planLabel = account.plan_type
    ? PLAN_LABEL_KEYS[account.plan_type]
    : undefined
  const provider =
    credentialText(cred.provider) ?? credentialText(cred.type) ?? '-'
  const title = planLabel
    ? `${provider.charAt(0).toUpperCase()}${provider.slice(1)} · ${planLabel}`
    : provider
  const identity =
    credentialText(cred.email) ?? credentialText(cred.label) ?? account.name
  const socksAlert = props.alerts.find((a) => a.kind.startsWith('socks_'))
  const stale = socks !== 'ok' && !!account.quota_observed_at
  // Only one notice at a time, most urgent first.
  let notice: 'socks' | 'missing' | 'resets' | null = null
  if (socksAlert || (disabled && account.auto_disabled_at)) notice = 'socks'
  else if (socks === 'missing' && account.is_claude) notice = 'missing'
  else if (hasResets) notice = 'resets'

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })

  const refreshMutation = useMutation({
    mutationFn: () => refreshAccount(ref),
    onSuccess: () => invalidate(),
    onError: (error) => handleServerError(error),
  })

  const actionMutation = useMutation({
    mutationFn: async (action: Exclude<PendingAction, null>) => {
      if (action === 'enable') return setAccountDisabled(ref, false)
      if (action === 'disable') return setAccountDisabled(ref, true)
      if (action === 'token') return refreshAccountToken(ref)
      return claimResetGrant(ref)
    },
    onSuccess: (data, action) => {
      setPending(null)
      if (action === 'reset' && data && typeof data === 'object') {
        const result = (data as { result: string }).result
        if (result === 'reset' || result === 'already_used') {
          toast.success(t('Reset grant claimed: {{result}}', { result }))
        } else {
          toast.warning(t('Reset grant result: {{result}}', { result }))
        }
      } else {
        toast.success(t('Operation succeeded'))
      }
      invalidate()
    },
    onError: (error) => {
      setPending(null)
      handleServerError(error)
    },
  })

  const confirmCopy = {
    enable: {
      title: t('Enable this account?'),
      desc: t('CPA will start routing requests to {{name}} again.', {
        name: account.name,
      }),
    },
    disable: {
      title: t('Disable this account?'),
      desc: t('CPA will stop routing requests to {{name}}.', {
        name: account.name,
      }),
    },
    token: {
      title: t('Refresh token now?'),
      desc: t(
        'CPA will force a token refresh for {{name}}. The token itself is never sent to this page.',
        { name: account.name }
      ),
    },
    reset: {
      title: t('Use one reset grant?'),
      desc: t(
        'This spends one Claude reset grant of {{name}} ({{left}} left) and clears its rate limit. This cannot be undone.',
        { name: account.name, left: resetsLeft }
      ),
    },
  }

  const warning =
    credentialText(cred.refresh_error) ??
    (cred.requires_sign_in === true ? t('Sign-in required') : null)

  return (
    <div
      className={cn(
        'bg-card flex w-[300px] flex-none flex-col rounded-lg border px-3.5 py-3 text-[13px] shadow-xs',
        hasResets && 'border-red-500',
        disabled && 'opacity-90'
      )}
    >
      {/* Header */}
      <div className='flex items-start justify-between gap-2'>
        <div className='min-w-0'>
          <div className='truncate font-medium' title={account.name}>
            {title}
          </div>
          <div
            className='text-muted-foreground truncate text-xs'
            title={`${identity} · ${account.name} · ${props.instanceName}`}
          >
            {identity}
          </div>
        </div>
        <div className='flex flex-none items-center gap-1.5'>
          <SocksBadge account={account} accountRef={ref} />
          <Switch
            checked={!disabled}
            aria-label={t('Enabled')}
            disabled={actionMutation.isPending}
            onCheckedChange={(checked) =>
              setPending(checked ? 'enable' : 'disable')
            }
          />
        </div>
      </div>

      <div className='mt-1'>
        <StatsLine
          account={account}
          usage={props.usage}
          collecting={props.collecting}
        />
      </div>

      {/* Notices */}
      {notice === 'socks' && (
        <Notice tone='red'>
          {t('Proxy unreachable, account disabled automatically')}
          <br />
          <span className='text-red-700 dark:text-red-300'>
            {formatTimestampToDate(
              socksAlert?.last_seen_at ?? account.auto_disabled_at ?? 0
            )}{' '}
            {socksAlert
              ? t('check failed · waiting for your confirmation')
              : t('check failed')}
          </span>
        </Notice>
      )}
      {notice === 'missing' && (
        <Notice tone='amber'>
          {t('No socks5 proxy configured, quota checks are blocked')}
          <br />
          {t('Chats relayed by CPA still go out directly')}
        </Notice>
      )}
      {notice === 'resets' && (
        <Notice tone='red'>
          {t('Reset grants: {{left}} / {{total}}', {
            left: resetsLeft,
            total: resetsTotal,
          })}
          {nextEnds ? ` · ${t('Ends')} ${formatIso(nextEnds)}` : ''}
        </Notice>
      )}
      {warning && <Notice tone='amber'>{warning}</Notice>}

      {/* Quota */}
      {account.is_claude && (
        <div className='mt-1'>
          {account.quota_error && socks === 'ok' && (
            <div className='mb-1 text-xs text-red-600 dark:text-red-400'>
              {account.quota_observed_at
                ? t('Refresh failed. Showing last successful check: {{time}}', {
                    time: formatTimestampToDate(account.quota_observed_at),
                  })
                : t('Refresh failed: {{error}}', {
                    error: account.quota_error,
                  })}
            </div>
          )}
          {extra && extra.isEnabled && (
            <div className='text-muted-foreground mb-1 text-xs'>
              {t('Extra usage')}:{' '}
              {t('Spent {{spent}} / limit {{limit}}', {
                spent: formatMoney(
                  extra.usedCredits === null ? null : extra.usedCredits / 100,
                  extra.currency ?? 'USD'
                ),
                limit: formatMoney(
                  extra.monthlyLimit === null ? null : extra.monthlyLimit / 100,
                  extra.currency ?? 'USD'
                ),
              })}
            </div>
          )}
          {dollarWindows.map((balance) => (
            <div key={balance.key} className='text-muted-foreground text-xs'>
              {t('Balance')} ({balance.key}):{' '}
              {formatMoney(balance.remainingDollars)} /{' '}
              {formatMoney(balance.limitDollars)}
            </div>
          ))}
          {(socks !== 'missing' || stale) &&
            windows.map((window) => {
              const remaining = remainingPercent(window.usedPercent)
              const level = stale ? 'unknown' : quotaLevel(remaining)
              return (
                <div
                  key={window.id}
                  title={`${t('Resets at')} ${formatIso(window.resetsAt)}`}
                >
                  <div className='text-muted-foreground flex justify-between text-xs'>
                    <span>{t(window.labelKey)}</span>
                    <span className={LEVEL_TEXT_CLASS[level]}>
                      {remainingLabel(remaining, stale, t)}
                    </span>
                  </div>
                  <div className='bg-muted mt-0.5 mb-1.5 h-[5px] overflow-hidden rounded-full'>
                    <div
                      className={cn('h-full', LEVEL_BAR_CLASS[level])}
                      style={{ width: `${remaining ?? 0}%` }}
                    />
                  </div>
                </div>
              )
            })}
          {socks === 'ok' && !account.quota_error && windows.length === 0 && (
            <div className='text-muted-foreground text-xs'>
              {account.quota_checked_at
                ? t('No usage windows returned')
                : t('Not checked yet, click refresh')}
            </div>
          )}
        </div>
      )}

      {/* Actions */}
      <div className='mt-auto flex items-center gap-1.5 border-t pt-2.5'>
        <Button size='xs' variant='outline' onClick={() => setModelsOpen(true)}>
          {t('Models')}
        </Button>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                size='xs'
                variant='outline'
                className={cn(proxyReady && RED_OUTLINE)}
                disabled={!proxyReady}
                onClick={() => setPending('token')}
              />
            }
          >
            {t('Refresh token')}
          </TooltipTrigger>
          <TooltipContent className='max-w-60'>
            {proxyReady
              ? t(
                  'Asks CPA to renew the login token through this account’s SOCKS proxy. Only needed when requests fail with auth errors.'
                )
              : t('Needs a working socks5 proxy')}
          </TooltipContent>
        </Tooltip>
        {account.is_claude && (
          <Button
            size='xs'
            variant='outline'
            className={cn(hasResets && proxyReady && RED_OUTLINE)}
            disabled={!hasResets || !proxyReady}
            onClick={() => setPending('reset')}
          >
            {t('Use reset grant')}
          </Button>
        )}
        <span className='flex-1' />
        <Button
          size='xs'
          variant='outline'
          disabled={refreshMutation.isPending}
          onClick={() => refreshMutation.mutate()}
        >
          {refreshMutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <RefreshCw />
          )}
          {t('Refresh')}
        </Button>
      </div>

      {pending && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open && !actionMutation.isPending) setPending(null)
          }}
          title={confirmCopy[pending].title}
          desc={confirmCopy[pending].desc}
          destructive={pending !== 'enable'}
          isLoading={actionMutation.isPending}
          handleConfirm={() => actionMutation.mutate(pending)}
        />
      )}
      {modelsOpen && (
        <ModelsDialog
          open={modelsOpen}
          onOpenChange={setModelsOpen}
          accountRef={ref}
        />
      )}
    </div>
  )
}

function Notice(props: { tone: 'red' | 'amber'; children: React.ReactNode }) {
  return (
    <div
      className={cn(
        'my-2 rounded-md px-2 py-1.5 text-xs leading-normal break-all',
        props.tone === 'red'
          ? 'bg-red-50 text-red-900 dark:bg-red-500/15 dark:text-red-200'
          : 'bg-amber-50 text-amber-900 dark:bg-amber-500/15 dark:text-amber-200'
      )}
    >
      {props.children}
    </div>
  )
}

function remainingLabel(
  remaining: number | null,
  stale: boolean,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  if (remaining === null) return '-'
  const percent = Math.round(remaining)
  return stale
    ? t('Last {{percent}}%', { percent })
    : t('{{percent}}% left', { percent })
}
