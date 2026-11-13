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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import dayjs from '@/lib/dayjs'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  OVERVIEW_QUERY_KEY,
  claimResetGrant,
  refreshAccount,
  setAccountDisabled,
  setAccountShift,
} from '../api'
import { isProxyBlocked, quotaLive, socksStateOf } from '../lib/card'
import {
  PLAN_LABEL_KEYS,
  buildEffectiveQuota,
  buildResetGrants,
  quotaLevel,
  remainingPercent,
  resetButtonHint,
  type ResetButtonHint,
} from '../lib/quota'
import {
  BLOCK_LABEL_KEYS,
  QUEUE_REASON_KEYS,
  REASON_LABEL_KEYS,
  REST_REASON_KEYS,
  SHIFT_LABEL_KEYS,
  rateText,
} from '../lib/rotation'
import type {
  CpaAccount,
  CpaAccountStats,
  CpaAlert,
  CpaCardUsage,
  CpaRate,
  CpaRotationState,
  ShiftKind,
} from '../types'
import { AccountDetails } from './account-details'
import { ModelsDialog } from './models-dialog'
import { QuotaSourceLine } from './quota-source-line'
import { ResetConfirmDialog } from './reset-confirm-dialog'
import { ResetCountdownLabel } from './reset-countdown'
import { SocksBadge } from './socks-badge'
import { StatsLine } from './stats-line'

type PendingAction = 'enable' | 'disable' | 'reset' | null

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

/**
 * Claude cards use Anthropic's warm palette (design option A, 2026-11-05):
 * ivory surface, clay accents, serif title. Quota bars keep green / amber /
 * red and the SOCKS badge keeps its colors, so safety states stay readable.
 */
const CLAUDE_CARD =
  'border-[#E8E6DC] bg-[#FAF9F5] dark:border-[#45443F] dark:bg-[#30302E]'
const CLAUDE_BADGE =
  'bg-[#F5E6DD] text-[#B05730] dark:bg-[#4A3328] dark:text-[#E8A385]'
const CLAUDE_SWITCH = 'data-checked:bg-[#D97757]'
const CLAUDE_TRACK = 'bg-[#EDEBE3] dark:bg-[#45443F]'

/** Claude spark mark (simplified, drawn in clay). */
function ClaudeMark() {
  return (
    <svg
      viewBox='0 0 24 24'
      className='size-4 flex-none'
      aria-hidden='true'
      stroke='#D97757'
      strokeWidth={2.6}
      strokeLinecap='round'
    >
      <line x1='12' y1='2.5' x2='12' y2='21.5' />
      <line x1='2.5' y1='12' x2='21.5' y2='12' />
      <line x1='5.3' y1='5.3' x2='18.7' y2='18.7' />
      <line x1='18.7' y1='5.3' x2='5.3' y2='18.7' />
    </svg>
  )
}

const RED_OUTLINE =
  'border-red-500 text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-500/10'

function formatIso(value: string | null | undefined) {
  if (!value) return '-'
  const ms = Date.parse(value)
  if (Number.isNaN(ms)) return '-'
  return formatTimestampToDate(ms, 'milliseconds')
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
  /** 30-day per-account totals from GET /stats; null while loading. */
  stats: CpaAccountStats | null
  collecting: boolean
  alerts: CpaAlert[]
  /** This account's row of the pool rotation; null before the first run. */
  rotation?: CpaRotationState | null
  rotationEnabled?: boolean
  /** Off: every rotation mark on the card is hidden (rotation keeps running). */
  rotationBadges?: boolean
  /** RPM / TPM of this account; null when the usage queue is not collected. */
  rate?: CpaRate | null
}

/** Hover delay before the details popover opens (agreed design: 0.3 s). */
const DETAILS_OPEN_DELAY_MS = 300

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
  // Token refresh still requires a working SOCKS proxy.
  const proxyReady = socks === 'ok'
  // Quota queries also run without any proxy_url (direct test accounts).
  const live = quotaLive(account)
  const blocked = isProxyBlocked(account)
  const claude = account.is_claude
  // Optimization C: no proxy_url at all = direct test account.
  const direct = claude && !account.proxy_url && !blocked

  const quota = buildEffectiveQuota(account, Date.now())
  const windows = quota.windows
  const fromChat = quota.source === 'chat'
  const grants = buildResetGrants(account.reset_grants)
  const resetsLeft = grants?.resetsLeft ?? 0
  const hasResets = resetsLeft > 0
  const activeGrants = (grants?.grants ?? []).filter((g) => g.resetsLeft > 0)
  const resetsTotal = activeGrants.reduce((sum, g) => sum + g.resetsTotal, 0)
  // Reset grants: SOCKS ok, or a direct account (no proxy_url at all) whose
  // claim CPA sends directly (design draft 2026-11-11). Rules 2-5 only add
  // a hint; the server makes the final call (user decision Q1 = b).
  const resetReady = proxyReady || direct
  const resetHint = resetReady
    ? resetButtonHint(account.reset_grants, windows, Date.now())
    : null
  const nextEnds = activeGrants
    .map((g) => g.endsAt)
    .filter((v): v is string => !!v)
    .sort()[0]
  const planLabel = account.plan_type
    ? PLAN_LABEL_KEYS[account.plan_type]
    : undefined
  const provider =
    credentialText(cred.provider) ?? credentialText(cred.type) ?? '-'
  const title = `${provider.charAt(0).toUpperCase()}${provider.slice(1)}`
  const identity =
    credentialText(cred.email) ?? credentialText(cred.label) ?? account.name
  const socksAlert = props.alerts.find((a) => a.kind.startsWith('socks_'))
  // Chat-observed quota is real upstream data even when the proxy check
  // failed later, so it is never greyed as stale.
  const stale = !live && !fromChat && !!account.quota_observed_at
  // Notices stack, most urgent first (design draft 2026-11-11, option C):
  // a SOCKS notice no longer hides the reset-grant one.
  const showSocksNotice =
    !!socksAlert || (disabled && !!account.auto_disabled_at)
  // Direct accounts need no notice: the amber "Direct" badge says it
  // (design draft 2026-11-11, B). Only an invalid proxy_url is a fault.
  const showMissingNotice =
    !showSocksNotice && socks === 'missing' && blocked && account.is_claude
  const showResetsNotice = hasResets
  // Pool rotation (design draft 2026-11-11): only when the switch is on.
  const rot =
    props.rotationEnabled && props.rotationBadges !== false
      ? (props.rotation ?? null)
      : null
  const resting = rot?.state === 'resting'
  const queued = rot?.state === 'queued'
  // Dry run: still online, would be resting if the dry run were off.
  const wouldRest = rot?.state === 'online' && rot.would_rest === true
  const working =
    rot?.state === 'online' && rot.participates && rot.worked_hours > 0
  const held = !!rot?.hold_until && rot.hold_until * 1000 > Date.now()
  // 401 / 403 stop: shown even with the rotation marks hidden.
  const authStopped = disabled && !!account.auth_disabled_at
  const rate = props.rate ?? null

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })

  const shiftMutation = useMutation({
    mutationFn: (shift: ShiftKind) => setAccountShift(ref, shift),
    onSuccess: () => {
      toast.success(t('Saved'))
      invalidate()
    },
    onError: (error) => handleServerError(error),
  })

  const refreshMutation = useMutation({
    mutationFn: () => refreshAccount(ref),
    onSuccess: () => invalidate(),
    onError: (error) => handleServerError(error),
  })

  const actionMutation = useMutation({
    mutationFn: async (action: Exclude<PendingAction, null>) => {
      if (action === 'enable') return setAccountDisabled(ref, false)
      if (action === 'disable') return setAccountDisabled(ref, true)
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
  }

  const warning =
    credentialText(cred.refresh_error) ??
    (cred.requires_sign_in === true ? t('Sign-in required') : null)

  return (
    <HoverCard>
      <HoverCardTrigger
        delay={DETAILS_OPEN_DELAY_MS}
        closeDelay={150}
        render={
          <div
            className={cn(
              'bg-card flex h-full w-[300px] flex-col rounded-lg border px-3.5 py-3 text-[13px] shadow-xs',
              claude && CLAUDE_CARD,
              hasResets && 'border-red-500',
              disabled && 'opacity-90'
            )}
          />
        }
      >
        {/* Header */}
        <div className='flex items-start justify-between gap-2'>
          <div className='min-w-0'>
            <div className='flex min-w-0 items-center gap-1.5'>
              {claude && <ClaudeMark />}
              <span
                className={cn(
                  'truncate font-medium',
                  claude && 'font-serif text-[15px]'
                )}
                title={account.name}
              >
                {title}
              </span>
              {planLabel && (
                <Badge
                  variant='secondary'
                  className={cn(
                    'h-[18px] px-1.5 text-[11px]',
                    claude && CLAUDE_BADGE
                  )}
                >
                  {planLabel}
                </Badge>
              )}
              {resting && (
                <Badge
                  variant='secondary'
                  className='h-[18px] bg-sky-100 px-1.5 text-[11px] text-sky-700 dark:bg-sky-500/20 dark:text-sky-300'
                >
                  {t('Resting')}
                </Badge>
              )}
              {rot?.shift && (
                <ShiftBadge
                  shift={rot.shift}
                  pending={shiftMutation.isPending}
                  onChange={(shift) => shiftMutation.mutate(shift)}
                />
              )}
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
              className={cn(claude && CLAUDE_SWITCH)}
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
          {rate && <RateLine rate={rate} />}
          {working && (
            <div className='text-muted-foreground mt-0.5 text-xs'>
              {t('Working {{hours}} h in a row', { hours: rot.worked_hours })}
              {held &&
                ` · ${t('Woken early, no rest before {{time}}', {
                  time: dayjs.unix(rot.hold_until ?? 0).format('HH:mm'),
                })}`}
            </div>
          )}
        </div>

        {/* Notices */}
        {authStopped && (
          <Notice tone='red'>
            {t('Upstream returned 401 / 403, account disabled automatically')}
            <br />
            <span className='text-red-700 dark:text-red-300'>
              {formatTimestampToDate(account.auth_disabled_at ?? 0)}{' '}
              {t('Re-enable it after the sign-in is fixed')}
            </span>
          </Notice>
        )}
        {resting && (
          <Notice tone='sky'>
            {'💤 '}
            {t(REST_REASON_KEYS[rot.reason ?? 'rotation'], {
              hours: rot.rest_worked_hours ?? rot.worked_hours,
            })}
            <div className='mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1'>
              {rot.rest_until ? (
                <span className='text-sky-700 dark:text-sky-300'>
                  {t('Back online automatically at {{time}}', {
                    time: dayjs.unix(rot.rest_until).format('HH:mm'),
                  })}
                </span>
              ) : null}
              <Button
                size='xs'
                variant='outline'
                className='h-5 border-sky-500 bg-transparent px-1.5 text-[11px] text-sky-700 hover:bg-sky-100 dark:text-sky-300 dark:hover:bg-sky-500/20'
                disabled={actionMutation.isPending}
                onClick={() => setPending('enable')}
              >
                {t('Back online now')}
              </Button>
            </div>
          </Notice>
        )}
        {queued && (
          <Notice tone='gray'>
            {t(QUEUE_REASON_KEYS[rot.reason ?? 'rotation'], {
              pos: rot.queue_pos ?? 0,
              hours: rot.worked_hours,
            })}
            {rot.blocked && (
              <div className='mt-0.5'>
                {t('Held back: {{why}}', {
                  why: t(BLOCK_LABEL_KEYS[rot.blocked]),
                })}
              </div>
            )}
          </Notice>
        )}
        {wouldRest && (
          <Notice tone='amber'>
            {t('Dry run: would be resting now ({{reason}}), still online', {
              reason: t(REASON_LABEL_KEYS[rot.reason ?? 'rotation']),
            })}
          </Notice>
        )}
        {showSocksNotice && (
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
        {showMissingNotice && (
          <Notice tone='amber'>
            {t('socks5 proxy is invalid, quota checks are blocked')}
          </Notice>
        )}
        {showResetsNotice && (
          <Notice tone='violet'>
            {'🎫 '}
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
            {account.quota_error && live && (
              <div className='mb-1 text-xs text-red-600 dark:text-red-400'>
                {account.quota_observed_at
                  ? t(
                      'Refresh failed. Showing last successful check: {{time}}',
                      {
                        time: formatTimestampToDate(account.quota_observed_at),
                      }
                    )
                  : t('Refresh failed: {{error}}', {
                      error: account.quota_error,
                    })}
              </div>
            )}
            {(live || stale || fromChat) &&
              windows.map((window) => {
                const remaining = remainingPercent(window.usedPercent)
                const level = stale ? 'unknown' : quotaLevel(remaining)
                return (
                  <div
                    key={window.id}
                    title={`${t('Resets at')} ${formatIso(window.resetsAt)}`}
                  >
                    <div className='text-muted-foreground flex justify-between gap-2 text-xs'>
                      <span className='truncate'>
                        {t(window.labelKey)}
                        {window.resetDone ? (
                          <span className='text-muted-foreground/80'>
                            {' · '}
                            {t('Reset done')}
                          </span>
                        ) : (
                          <ResetCountdownLabel
                            resetsAt={window.resetsAt}
                            stale={stale}
                          />
                        )}
                        {window.limited && (
                          <span className='ml-1 rounded border border-red-200 bg-red-50 px-1 text-[11px] text-red-600 dark:border-red-500/30 dark:bg-red-500/15 dark:text-red-300'>
                            {t('Rate limited')}
                          </span>
                        )}
                      </span>
                      <span className={cn('shrink-0', LEVEL_TEXT_CLASS[level])}>
                        {remainingLabel(remaining, stale, t)}
                      </span>
                    </div>
                    <div
                      className={cn(
                        'mt-0.5 mb-1.5 h-[5px] overflow-hidden rounded-full',
                        claude ? CLAUDE_TRACK : 'bg-muted'
                      )}
                    >
                      <div
                        className={cn('h-full', LEVEL_BAR_CLASS[level])}
                        style={{ width: `${remaining ?? 0}%` }}
                      />
                    </div>
                  </div>
                )
              })}
            {(live || stale || fromChat) && windows.length > 0 && (
              <QuotaSourceLine
                source={quota.source}
                observedAt={quota.observedAt}
              />
            )}
            {live && !account.quota_error && windows.length === 0 && (
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
          <Button
            size='xs'
            variant='outline'
            onClick={() => setModelsOpen(true)}
          >
            {t('Models')}
          </Button>
          {account.is_claude && hasResets && (
            <ResetGrantButton
              ready={resetReady}
              hint={resetHint}
              onClick={() => setPending('reset')}
            />
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

        {pending === 'reset' && (
          <ResetConfirmDialog
            identity={identity}
            fileName={account.name}
            left={resetsLeft}
            direct={direct}
            loading={actionMutation.isPending}
            onCancel={() => {
              if (!actionMutation.isPending) setPending(null)
            }}
            onConfirm={() => actionMutation.mutate('reset')}
          />
        )}
        {pending && pending !== 'reset' && (
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
      </HoverCardTrigger>
      <HoverCardContent
        side='right'
        align='start'
        sideOffset={8}
        className='max-h-[80vh] w-[300px] overflow-y-auto p-3'
      >
        {rate && (
          <div className='mb-3 border-b pb-3'>
            <RateDetails rate={rate} />
          </div>
        )}
        <AccountDetails
          account={account}
          instanceName={props.instanceName}
          usage={props.usage}
          stats={props.stats}
          collecting={props.collecting}
        />
      </HoverCardContent>
    </HoverCard>
  )
}

/**
 * One-line RPM / TPM of the last minute, then the busiest minute of the
 * last hour (429s left out). The 5-minute average is in the hover details.
 */
function RateLine(props: { rate: CpaRate }) {
  const { t } = useTranslation()
  const r = props.rate
  return (
    <div className='text-muted-foreground mt-0.5 truncate text-[11px] tabular-nums'>
      RPM <b className='text-foreground font-medium'>{rateText(r.rpm_1m)}</b>
      {' · '}
      TPM <b className='text-foreground font-medium'>{rateText(r.tpm_1m)}</b>
      <span
        className='opacity-80'
        title={t('Busiest minute of the last hour, 429s not counted')}
      >
        {'　'}
        {t('Peak RPM')} {rateText(r.rpm_peak_1h)}
      </span>
    </div>
  )
}

/** Rate block at the top of the hover details. */
function RateDetails(props: { rate: CpaRate }) {
  const { t } = useTranslation()
  const r = props.rate
  const rows: [string, string][] = [
    [t('RPM last minute'), rateText(r.rpm_1m)],
    [t('TPM last minute'), rateText(r.tpm_1m)],
    [t('RPM 5 min avg'), rateText(r.rpm_5m)],
    [t('TPM 5 min avg'), rateText(r.tpm_5m)],
    [t('Peak RPM, last hour (no 429)'), rateText(r.rpm_peak_1h)],
    [t('Peak TPM, last hour'), rateText(r.tpm_peak_1h ?? 0)],
    [t('429 in the last hour'), String(r.r429_1h)],
  ]
  return (
    <div>
      <div className='mb-1 text-xs font-medium'>{t('Request rate')}</div>
      <div className='grid grid-cols-2 gap-x-3 gap-y-0.5 text-xs'>
        {rows.map(([label, value]) => (
          <div key={label} className='flex justify-between gap-2'>
            <span className='text-muted-foreground truncate'>{label}</span>
            <span className='tabular-nums'>{value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

const SHIFT_BADGE_CLASS: Record<ShiftKind, string> = {
  day: 'bg-amber-100 text-amber-800 dark:bg-amber-500/20 dark:text-amber-200',
  night:
    'bg-indigo-100 text-indigo-800 dark:bg-indigo-500/20 dark:text-indigo-200',
  all: 'bg-muted text-muted-foreground',
}

/** Shift badge; click to move the account to another shift. */
function ShiftBadge(props: {
  shift: ShiftKind
  pending: boolean
  onChange: (shift: ShiftKind) => void
}) {
  const { t } = useTranslation()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={props.pending}
        render={
          <button
            type='button'
            className={cn(
              'inline-flex h-[18px] flex-none items-center rounded-md px-1.5 text-[11px] font-medium',
              SHIFT_BADGE_CLASS[props.shift]
            )}
            title={t('Click to change the shift')}
          />
        }
      >
        {t(SHIFT_LABEL_KEYS[props.shift])}
      </DropdownMenuTrigger>
      <DropdownMenuContent align='start'>
        <DropdownMenuRadioGroup
          value={props.shift}
          onValueChange={(value) => {
            if (value !== props.shift) props.onChange(value as ShiftKind)
          }}
        >
          {(['day', 'night', 'all'] as ShiftKind[]).map((shift) => (
            <DropdownMenuRadioItem key={shift} value={shift}>
              {t(SHIFT_LABEL_KEYS[shift])}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * Reset grant button. A disabled button gets no pointer events, so the
 * tooltip sits on a wrapping span: the reason shows even when greyed.
 */
function ResetGrantButton(props: {
  ready: boolean
  hint: ResetButtonHint | null
  onClick: () => void
}) {
  const { t } = useTranslation()
  const reason = props.ready
    ? props.hint && resetHintText(props.hint, t)
    : t('Needs a working socks5 proxy')
  const button = (
    <Button
      size='xs'
      variant='outline'
      className={cn(props.ready && RED_OUTLINE)}
      disabled={!props.ready}
      onClick={props.onClick}
    >
      {t('Use reset grant')}
    </Button>
  )
  if (!reason) return button
  return (
    <Tooltip>
      <TooltipTrigger
        delay={150}
        render={
          <span className='inline-flex' tabIndex={props.ready ? -1 : 0} />
        }
      >
        {button}
      </TooltipTrigger>
      <TooltipContent className='max-w-64'>{reason}</TooltipContent>
    </Tooltip>
  )
}

function shortTime(iso: string): string {
  return dayjs(iso).format('MM-DD HH:mm')
}

function resetHintText(
  hint: ResetButtonHint,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  const tail = t('Last refresh says so; the server checks again on click')
  switch (hint.kind) {
    case 'ineligible':
      return `${t('This account cannot use reset grants right now')} · ${tail}`
    case 'cooldown':
      return `${t('Cooling down, usable again after {{time}}', { time: shortTime(hint.until) })} · ${tail}`
    case 'paused':
      return `${t('Reset grant paused by Anthropic')} · ${tail}`
    case 'not_started':
      return `${t('Reset grant starts at {{time}}', { time: shortTime(hint.startsAt) })} · ${tail}`
    case 'expired':
      return `${t('Reset grant expired')} · ${tail}`
    case 'not_usable':
      return `${t('Reset grant not usable right now')} · ${tail}`
    case 'not_limited':
      return `${t('Quota not used up yet; this grant works only after the limit is hit')} · ${tail}`
  }
}

const NOTICE_TONES = {
  red: 'bg-red-50 text-red-900 dark:bg-red-500/15 dark:text-red-200',
  amber: 'bg-amber-50 text-amber-900 dark:bg-amber-500/15 dark:text-amber-200',
  violet:
    'bg-violet-50 text-violet-900 dark:bg-violet-500/15 dark:text-violet-200',
  sky: 'bg-sky-50 text-sky-900 dark:bg-sky-500/15 dark:text-sky-200',
  gray: 'bg-muted text-muted-foreground',
} as const

function Notice(props: {
  tone: keyof typeof NOTICE_TONES
  children: React.ReactNode
}) {
  return (
    <div
      className={cn(
        'my-2 rounded-md px-2 py-1.5 text-xs leading-normal break-all',
        NOTICE_TONES[props.tone]
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
