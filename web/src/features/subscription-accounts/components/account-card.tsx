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
import {
  AlertTriangle,
  Boxes,
  KeyRound,
  Loader2,
  RefreshCw,
  RotateCcw,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
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
import {
  PLAN_LABEL_KEYS,
  buildDollarWindows,
  buildExtraUsage,
  buildQuotaWindows,
  buildResetGrants,
  quotaLevel,
  remainingPercent,
} from '../lib/quota'
import type { CpaAccount } from '../types'
import { ModelsDialog } from './models-dialog'

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

  const windows = buildQuotaWindows(account.usage)
  const dollarWindows = buildDollarWindows(account.usage)
  const extra = buildExtraUsage(account.usage)
  const grants = buildResetGrants(account.reset_grants)
  const resetsLeft = grants?.resetsLeft ?? 0
  const hasResets = resetsLeft > 0
  const planLabel = account.plan_type
    ? PLAN_LABEL_KEYS[account.plan_type]
    : undefined

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })

  const refreshMutation = useMutation({
    mutationFn: () => refreshAccount(ref),
    onSuccess: () => {
      invalidate()
    },
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

  const statusText =
    credentialText(cred.status_message) ?? credentialText(cred.status)
  const warning =
    credentialText(cred.refresh_error) ??
    (cred.requires_sign_in === true ? t('Sign-in required') : null)

  return (
    <div
      className={cn(
        'bg-card flex flex-col gap-3 rounded-lg border p-4 shadow-xs',
        hasResets && 'border-red-500 ring-1 ring-red-500/40',
        disabled && 'opacity-70'
      )}
    >
      {/* Identity */}
      <div className='flex items-start justify-between gap-2'>
        <div className='min-w-0 space-y-1'>
          <div className='flex flex-wrap items-center gap-1.5'>
            <Badge variant='outline'>
              {credentialText(cred.provider) ??
                credentialText(cred.type) ??
                '-'}
            </Badge>
            {planLabel && <Badge variant='secondary'>{planLabel}</Badge>}
            {disabled && <Badge variant='destructive'>{t('Disabled')}</Badge>}
            {cred.unavailable === true && (
              <Badge variant='warning'>{t('Unavailable')}</Badge>
            )}
          </div>
          <div className='truncate font-medium' title={account.name}>
            {credentialText(cred.label) ??
              credentialText(cred.email) ??
              account.name}
          </div>
          <div className='text-muted-foreground truncate text-xs'>
            {account.name} · {props.instanceName}
          </div>
        </div>
        <Switch
          checked={!disabled}
          aria-label={t('Enabled')}
          disabled={actionMutation.isPending}
          onCheckedChange={(checked) =>
            setPending(checked ? 'enable' : 'disable')
          }
        />
      </div>

      {/* Warnings */}
      {warning && (
        <div className='flex items-start gap-1.5 rounded-md bg-amber-50 px-2 py-1.5 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-300'>
          <AlertTriangle className='mt-0.5 size-3.5 shrink-0' />
          <span className='break-all'>{warning}</span>
        </div>
      )}

      {/* Meta rows */}
      <div className='text-muted-foreground grid grid-cols-2 gap-x-3 gap-y-1 text-xs'>
        <span>
          {t('Status')}: {statusText ?? '-'}
        </span>
        <span>
          {t('Requests')}: {credentialText(cred.success) ?? 0} /{' '}
          <span className='text-red-600 dark:text-red-400'>
            {credentialText(cred.failed) ?? 0}
          </span>
        </span>
        <span className='col-span-2 truncate' title={account.proxy_url}>
          {t('Proxy')}:{' '}
          {account.proxy_url ||
            (account.proxy_error ? t('Unknown') : t('Global / direct'))}
        </span>
        <span className='col-span-2'>
          {t('Last refresh')}: {formatIso(credentialText(cred.last_refresh))}
        </span>
      </div>

      {/* Quota */}
      {account.is_claude && (
        <div className='space-y-2 border-t pt-3'>
          {account.quota_error && (
            <div className='text-xs text-red-600 dark:text-red-400'>
              {account.quota_observed_at
                ? t('Refresh failed. Showing last successful check: {{time}}', {
                    time: formatTimestampToDate(account.quota_observed_at),
                  })
                : t('Refresh failed: {{error}}', {
                    error: account.quota_error,
                  })}
            </div>
          )}

          <div
            className={cn(
              'flex items-center justify-between rounded-md px-2 py-1 text-sm',
              hasResets
                ? 'bg-red-50 font-semibold text-red-600 dark:bg-red-500/15 dark:text-red-400'
                : 'text-muted-foreground'
            )}
          >
            <span>{t('Reset grants left')}</span>
            <span>{grants ? resetsLeft : '--'}</span>
          </div>
          {grants && grants.grants.length > 0 && hasResets && (
            <ul className='space-y-0.5 text-xs text-red-600 dark:text-red-400'>
              {grants.grants
                .filter((grant) => grant.resetsLeft > 0)
                .map((grant, index) => (
                  <li key={grant.id}>
                    {grant.label || t('Batch {{index}}', { index: index + 1 })}:{' '}
                    {t('{{left}} / {{total}} resets left', {
                      left: grant.resetsLeft,
                      total: grant.resetsTotal,
                    })}
                    {grant.endsAt &&
                      ` · ${t('Expires')} ${formatIso(grant.endsAt)}`}
                  </li>
                ))}
            </ul>
          )}

          {extra && extra.isEnabled && (
            <div className='text-xs'>
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
            <div key={balance.key} className='text-xs'>
              {t('Balance')} ({balance.key}):{' '}
              {formatMoney(balance.remainingDollars)} /{' '}
              {formatMoney(balance.limitDollars)}
              {balance.resetsAt && ` · ${formatIso(balance.resetsAt)}`}
            </div>
          ))}

          {windows.map((window) => {
            const remaining = remainingPercent(window.usedPercent)
            const level = quotaLevel(remaining)
            return (
              <div key={window.id} className='space-y-1'>
                <div className='flex items-center justify-between text-xs'>
                  <span>{t(window.labelKey)}</span>
                  <span className={cn('font-medium', LEVEL_TEXT_CLASS[level])}>
                    {remaining === null
                      ? '-'
                      : t('{{percent}}% left', {
                          percent: Math.round(remaining),
                        })}
                  </span>
                </div>
                <div className='bg-muted h-1.5 overflow-hidden rounded-full'>
                  <div
                    className={cn(
                      'h-full rounded-full',
                      LEVEL_BAR_CLASS[level]
                    )}
                    style={{ width: `${remaining ?? 0}%` }}
                  />
                </div>
                <div className='text-muted-foreground text-[11px]'>
                  {t('Resets at')} {formatIso(window.resetsAt)}
                </div>
              </div>
            )
          })}
          {!account.quota_error && windows.length === 0 && (
            <div className='text-muted-foreground text-xs'>
              {account.quota_checked_at
                ? t('No usage windows returned')
                : t('Not checked yet, click refresh')}
            </div>
          )}
          {account.quota_observed_at ? (
            <div className='text-muted-foreground text-[11px]'>
              {t('Checked at')}{' '}
              {formatTimestampToDate(account.quota_observed_at)}
            </div>
          ) : null}
        </div>
      )}

      {/* Actions */}
      <div className='mt-auto flex flex-wrap gap-1.5 border-t pt-3'>
        <Button size='xs' variant='outline' onClick={() => setModelsOpen(true)}>
          <Boxes /> {t('Models')}
        </Button>
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
        <Button size='xs' variant='outline' onClick={() => setPending('token')}>
          <KeyRound /> {t('Refresh token')}
        </Button>
        {account.is_claude && (
          <Button
            size='xs'
            variant={hasResets ? 'destructive' : 'outline'}
            disabled={!hasResets}
            onClick={() => setPending('reset')}
          >
            <RotateCcw /> {t('Use reset grant')}
          </Button>
        )}
      </div>

      {pending && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open && !actionMutation.isPending) setPending(null)
          }}
          title={confirmCopy[pending].title}
          desc={confirmCopy[pending].desc}
          destructive={pending === 'disable' || pending === 'reset'}
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
