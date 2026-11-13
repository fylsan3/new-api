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
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  OVERVIEW_QUERY_KEY,
  previewRotation,
  setAccountShift,
  updateSettings,
} from '../api'
import {
  SHIFT_LABEL_KEYS,
  parseExcluded,
  parseShifts,
  rotationKey,
} from '../lib/rotation'
import type {
  CpaAccount,
  CpaRotationSummary,
  CpaSettings,
  CpaSettingsInput,
  RestReason,
  ShiftKind,
} from '../types'
import { EmergencyStopButton } from './emergency-stop'
import { RotationPreview } from './rotation-preview'

type RotationSettingsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  settings: CpaSettings
  /** Claude accounts that can take part, all CPAs (one global set). */
  accounts: CpaAccount[]
  instanceNames: Map<number, string>
  /** Last loop result: rule hit counts and who rests right now. */
  rotation?: CpaRotationSummary
}

const TABS = [
  { value: 'basics', labelKey: 'Basics' },
  { value: 'safety', labelKey: 'Safety gates' },
  { value: 'wake', labelKey: 'Busy wake-up' },
  { value: 'night', labelKey: 'Night' },
  { value: 'shift', labelKey: 'Shift plan' },
  { value: 'protect', labelKey: 'Protect single accounts' },
  { value: 'accounts', labelKey: 'Accounts' },
] as const

type BoolKey = {
  [K in keyof CpaSettings]: CpaSettings[K] extends boolean ? K : never
}[keyof CpaSettings]
type NumKey = {
  [K in keyof CpaSettings]: CpaSettings[K] extends number ? K : never
}[keyof CpaSettings]
type ClockKey =
  | 'rotation_night_start'
  | 'rotation_night_end'
  | 'rotation_day_shift_start'
  | 'rotation_day_shift_end'
  | 'rotation_night_shift_start'
  | 'rotation_night_shift_end'

const BOOL_KEYS: BoolKey[] = [
  'rotation_enabled',
  'rotation_show_badges',
  'rotation_wake_429_enabled',
  'rotation_wake_rpm_enabled',
  'rotation_wake_rest',
  'rotation_wake_shift',
  'rotation_night_enabled',
  'rotation_shift_enabled',
  'rotation_limit_enabled',
  'rotation_quota_enabled',
  'rotation_daily_enabled',
  'rotation_capacity_enabled',
  'rotation_dry_run',
]

const NUM_KEYS: NumKey[] = [
  'rotation_work_hours',
  'rotation_rest_minutes',
  'rotation_jitter_minutes',
  'rotation_max_rest_percent',
  'rotation_min_online',
  'rotation_wake_429_minutes',
  'rotation_wake_429_count',
  'rotation_wake_rpm',
  'rotation_wake_batch',
  'rotation_wake_cooldown_minutes',
  'rotation_wake_hold_minutes',
  'rotation_night_max_rest_percent',
  'rotation_shift_jitter_minutes',
  'rotation_limit_minutes',
  'rotation_limit_count',
  'rotation_limit_rest_minutes',
  'rotation_quota_percent',
  'rotation_daily_hours',
  'rotation_batch_size',
  'rotation_batch_gap_minutes',
  'rotation_capacity_rpm',
  'rotation_capacity_tpm',
  'rotation_capacity_429_minutes',
]

const CLOCK_KEYS: ClockKey[] = [
  'rotation_night_start',
  'rotation_night_end',
  'rotation_day_shift_start',
  'rotation_day_shift_end',
  'rotation_night_shift_start',
  'rotation_night_shift_end',
]

/** Shown in hours, stored in minutes. */
const HOUR_FIELDS = new Set<NumKey>(['rotation_rest_minutes'])

/**
 * Account rotation settings, one global set, one tab per topic: basics,
 * safety gates, busy wake-up, night, shifts, single-account protection,
 * accounts. Every extra rule has its own switch and shows how many
 * accounts meet it right now. Switching rotation on goes through a
 * preview of what would happen before anything is saved.
 */
export function RotationSettingsDialog(props: RotationSettingsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const s = props.settings

  const [bools, setBools] = useState(
    () =>
      Object.fromEntries(BOOL_KEYS.map((k) => [k, s[k]])) as Record<
        BoolKey,
        boolean
      >
  )
  const [nums, setNums] = useState(
    () =>
      Object.fromEntries(
        NUM_KEYS.map((k) => [k, String(HOUR_FIELDS.has(k) ? s[k] / 60 : s[k])])
      ) as Record<NumKey, string>
  )
  const [clocks, setClocks] = useState(
    () =>
      Object.fromEntries(CLOCK_KEYS.map((k) => [k, s[k]])) as Record<
        ClockKey,
        string
      >
  )
  const [excluded, setExcluded] = useState(() =>
    parseExcluded(s.rotation_excluded)
  )
  const savedShifts = parseShifts(s.rotation_shifts)
  const [shifts, setShifts] = useState(() => parseShifts(s.rotation_shifts))

  const [tab, setTab] = useState<string>('basics')
  // Set while the "what would happen" step is shown instead of the form.
  const [preview, setPreview] = useState<CpaRotationSummary | null>(null)

  const buildInput = (): CpaSettingsInput => {
    const numbers = Object.fromEntries(
      NUM_KEYS.map((k) => {
        const v = Number(nums[k]) || 0
        return [k, Math.round(HOUR_FIELDS.has(k) ? v * 60 : v)]
      })
    )
    return {
      ...bools,
      ...numbers,
      ...clocks,
      rotation_excluded: [...excluded].join('\n'),
    }
  }

  // Plans with the form as it is and changes nothing. Shift changes of
  // single accounts are saved separately and are not part of the preview.
  const previewMutation = useMutation({
    mutationFn: () => previewRotation(buildInput()),
    onSuccess: (data) => setPreview(data),
    onError: (error) => handleServerError(error),
  })

  const mutation = useMutation({
    mutationFn: async () => {
      await updateSettings(buildInput())
      for (const [key, shift] of shifts) {
        if (savedShifts.get(key) === shift) continue
        const at = key.indexOf('/')
        await setAccountShift(
          { instance_id: Number(key.slice(0, at)), name: key.slice(at + 1) },
          shift
        )
      }
    },
    onSuccess: () => {
      toast.success(t('Saved'))
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      // The loop runs right after saving; pick up its result.
      window.setTimeout(
        () => queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY }),
        2000
      )
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })

  const toggleAccount = (key: string, take: boolean) => {
    setExcluded((prev) => {
      const next = new Set(prev)
      if (take) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const taking = props.accounts.filter(
    (account) => !excluded.has(rotationKey(account))
  ).length

  const sw = (key: BoolKey, label?: string) => (
    <Switch
      checked={bools[key]}
      aria-label={label}
      onCheckedChange={(v) => setBools((prev) => ({ ...prev, [key]: v }))}
    />
  )
  const num = (key: NumKey, suffix: string, step = 1, wide = false) => (
    <NumberInput
      value={nums[key]}
      step={step}
      suffix={suffix}
      wide={wide}
      onChange={(v) => setNums((prev) => ({ ...prev, [key]: v }))}
    />
  )
  const clock = (key: ClockKey) => (
    <Input
      className='h-8 w-[92px]'
      type='time'
      value={clocks[key]}
      onChange={(e) =>
        setClocks((prev) => ({ ...prev, [key]: e.target.value }))
      }
    />
  )
  const h = t('hours')
  const m = t('minutes')
  const times = t('times')
  // How many accounts meet a rule right now, from the last loop run
  // (about every 30 seconds), whether or not the rule is switched on.
  const hits = props.rotation?.hits
  const hit = (reason: RestReason) =>
    hits ? <HitCount count={hits[reason] ?? 0} /> : null
  // Off → on: show what would happen first, save only after a second yes.
  const turningOn = bools.rotation_enabled && !s.rotation_enabled
  const busy = mutation.isPending || previewMutation.isPending
  let saveLabel = t('Save')
  if (preview) saveLabel = t('Confirm and switch on')
  else if (turningOn) saveLabel = t('Preview, then switch on')

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        preview ? t('Before rotation is switched on') : t('Rotation settings')
      }
      description={
        preview
          ? t(
              'This is what would happen right now with these settings. Nothing has changed yet.'
            )
          : t(
              'One set for all CPAs. Resting = disabled in CPA, the same switch as on the card.'
            )
      }
      contentClassName='sm:max-w-3xl'
      contentHeight='min(70vh, 720px)'
      footer={
        <div className='flex w-full justify-end gap-2'>
          {preview ? (
            <Button
              variant='outline'
              disabled={busy}
              onClick={() => setPreview(null)}
            >
              {t('Back')}
            </Button>
          ) : (
            <Button variant='outline' onClick={() => props.onOpenChange(false)}>
              {t('Cancel')}
            </Button>
          )}
          <Button
            disabled={busy}
            onClick={() => {
              if (turningOn && !preview) previewMutation.mutate()
              else mutation.mutate()
            }}
          >
            {busy && <Loader2 className='animate-spin' />}
            {saveLabel}
          </Button>
        </div>
      }
    >
      {preview && (
        <RotationPreview
          preview={preview}
          current={props.rotation}
          accounts={props.accounts}
          instanceNames={props.instanceNames}
          dryRun={bools.rotation_dry_run}
        />
      )}
      <Tabs
        value={tab}
        onValueChange={(v) => setTab(String(v))}
        className={cn('gap-3 text-sm', preview && 'hidden')}
      >
        <TabsList className='h-auto w-full flex-wrap justify-start'>
          {TABS.map((item) => (
            <TabsTrigger
              key={item.value}
              value={item.value}
              className='flex-none px-2.5 py-1'
            >
              {t(item.labelKey)}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value='basics' className='space-y-4'>
          <Group title={t('Basics')}>
            <Row
              label={t('Master switch')}
              hint={t('Off by default; rotation starts only when on')}
            >
              {sw('rotation_enabled', t('Master switch'))}
            </Row>
            <Row
              label={t('Show rotation marks on the page')}
              hint={t(
                'Off hides the summary bar and every rotation badge / notice on the cards; rotation keeps running. Reopen it here (button next to "Refresh all").'
              )}
            >
              {sw('rotation_show_badges')}
            </Row>
            <Row
              label={t('Rest after working this many hours in a row')}
              hint={t(
                'An hour counts only if it had requests. Counting starts when rotation is switched on.'
              )}
              badge={hit('rotation')}
            >
              {num('rotation_work_hours', h)}
            </Row>
            <Row
              label={t('Rest length')}
              hint={t('The real length varies by the jitter below')}
            >
              {num('rotation_rest_minutes', h, 0.5)}
            </Row>
            <Row
              label={t('Random jitter of the rest')}
              hint={t(
                'So accounts do not go offline at the same time every day'
              )}
            >
              {num('rotation_jitter_minutes', m)}
            </Row>
            <Row
              label={t('At most resting at once')}
              hint={t('Share of the Claude accounts of each CPA')}
            >
              {num('rotation_max_rest_percent', '%')}
            </Row>
            <Row
              label={t('Keep at least online')}
              hint={t(
                'Per CPA; below this no account goes to rest. Always kept, whatever rule below.'
              )}
            >
              {num('rotation_min_online', t('accounts'))}
            </Row>
          </Group>
          {s.rotation_enabled && (
            <div className='flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 px-3 py-2.5 dark:border-red-500/30'>
              <div className='min-w-0'>
                <div className='font-medium'>{t('Emergency')}</div>
                <div className='text-muted-foreground text-xs'>
                  {t(
                    'Switches rotation off and enables every account it sent resting, right away.'
                  )}
                </div>
              </div>
              <EmergencyStopButton
                size='sm'
                onDone={() => props.onOpenChange(false)}
              />
            </div>
          )}
        </TabsContent>

        <TabsContent value='safety' className='space-y-4'>
          <Group
            title={t('Going offline step by step')}
            hint={t(
              'Accounts never go offline all at once. Every rule obeys "At most resting at once" and "Keep at least online", shifts and quota rules included.'
            )}
          >
            <Row
              label={t('Accounts going offline in one step')}
              hint={t('Per CPA')}
            >
              {num('rotation_batch_size', t('accounts'))}
            </Row>
            <Row
              label={t('Wait between two steps')}
              hint={t(
                'The next account goes only after this, and only if the checks below still pass'
              )}
            >
              {num('rotation_batch_gap_minutes', m)}
            </Row>
          </Group>
          <Group
            title={t('Check the load before an account goes offline')}
            hint={t(
              'Looks at the busiest minute of the CPA in the last hour (429s left out) and asks: could the accounts left online carry it? If not, the account waits in the queue.'
            )}
            right={sw('rotation_capacity_enabled')}
          >
            <Row
              label={t('RPM each remaining account may carry')}
              hint={t('Busiest minute ÷ accounts left online. 0 = not checked')}
            >
              {num('rotation_capacity_rpm', 'RPM')}
            </Row>
            <Row
              label={t('TPM each remaining account may carry')}
              hint={t(
                'Input + output tokens, no cache, like the usage log. 0 = not checked'
              )}
            >
              {num('rotation_capacity_tpm', 'TPM', 1000, true)}
            </Row>
            <Row
              label={t('Nobody goes offline after a recent 429')}
              hint={t(
                'Any 429 of the CPA in the last N minutes holds everyone. 0 = not checked'
              )}
            >
              {num('rotation_capacity_429_minutes', m)}
            </Row>
          </Group>
          <Group
            title={t('Dry run')}
            hint={t(
              'Rotation plans as usual but takes nobody offline: cards and the summary bar only show who would be resting. Waking accounts up still works. Use it to try new settings safely.'
            )}
            right={sw('rotation_dry_run')}
          >
            <Row
              label={t('Would be resting right now')}
              hint={t('From the last run, about every 30 seconds')}
            >
              <span className='tabular-nums'>
                {props.rotation?.dry_run
                  ? t('{{n}} accounts', { n: props.rotation.would_rest ?? 0 })
                  : '-'}
              </span>
            </Row>
          </Group>
        </TabsContent>

        <TabsContent value='wake' className='space-y-4'>
          <Group
            title={t('Wake early when busy')}
            hint={t(
              'When the pool is busy, resting accounts come back before their time. Accounts stopped for safety (SOCKS, 401 / 403, by hand) are never woken.'
            )}
          >
            <Row
              label={t('By 429 count')}
              hint={t('429s of the whole CPA in the last N minutes')}
            >
              <div className='flex items-center gap-2'>
                {num('rotation_wake_429_minutes', m)}
                {num('rotation_wake_429_count', times)}
                {sw('rotation_wake_429_enabled')}
              </div>
            </Row>
            <Row
              label={t('By requests per account')}
              hint={t(
                'Average requests per minute of each online account, last 5 minutes'
              )}
            >
              <div className='flex items-center gap-2'>
                {num('rotation_wake_rpm', 'RPM')}
                {sw('rotation_wake_rpm_enabled')}
              </div>
            </Row>
            <Row
              label={t('Accounts woken each time')}
              hint={t('The one due back soonest goes first')}
            >
              {num('rotation_wake_batch', t('accounts'))}
            </Row>
            <Row
              label={t('Wait before checking again')}
              hint={t('After a wake-up, look again only after this')}
            >
              {num('rotation_wake_cooldown_minutes', m)}
            </Row>
            <Row
              label={t('A woken account stays online at least')}
              hint={t('No rotation rest during this time')}
            >
              {num('rotation_wake_hold_minutes', m)}
            </Row>
            <Row
              label={t('May wake accounts on a normal rotation rest')}
              hint=''
            >
              {sw('rotation_wake_rest')}
            </Row>
            <Row label={t('May wake accounts that are off shift')} hint=''>
              {sw('rotation_wake_shift')}
            </Row>
          </Group>
        </TabsContent>

        <TabsContent value='night' className='space-y-4'>
          <Group
            title={t('More rest at night')}
            hint={t(
              'In this window more accounts may rest at once (server time). "Keep at least online" still holds.'
            )}
            right={sw('rotation_night_enabled')}
          >
            <Row label={t('Night window')} hint=''>
              <div className='flex items-center gap-1.5'>
                {clock('rotation_night_start')}
                <span className='text-muted-foreground'>-</span>
                {clock('rotation_night_end')}
              </div>
            </Row>
            <Row label={t('At most resting at night')} hint=''>
              {num('rotation_night_max_rest_percent', '%')}
            </Row>
          </Group>
        </TabsContent>

        <TabsContent value='shift' className='space-y-4'>
          <Group
            title={t('Shifts')}
            hint={t(
              'Day / night / all day. New accounts get day and night in turn; change one on the "Accounts" tab or on its card.'
            )}
            badge={hit('shift')}
            right={sw('rotation_shift_enabled')}
          >
            <Row label={t('Day shift')} hint={t('On duty - off duty')}>
              <div className='flex items-center gap-1.5'>
                {clock('rotation_day_shift_start')}
                <span className='text-muted-foreground'>-</span>
                {clock('rotation_day_shift_end')}
              </div>
            </Row>
            <Row label={t('Night shift')} hint={t('On duty - off duty')}>
              <div className='flex items-center gap-1.5'>
                {clock('rotation_night_shift_start')}
                <span className='text-muted-foreground'>-</span>
                {clock('rotation_night_shift_end')}
              </div>
            </Row>
            <Row
              label={t('Random offset of on / off duty')}
              hint={t('Each account gets its own stable offset, plus or minus')}
            >
              {num('rotation_shift_jitter_minutes', m)}
            </Row>
          </Group>
        </TabsContent>

        <TabsContent value='protect' className='space-y-4'>
          <Group
            title={t('Protect single accounts')}
            hint={t(
              'Priority: safety stops > shifts > these rules > normal rotation.'
            )}
          >
            <Row
              label={t('Rest after many 429s')}
              hint={t('N minutes, at least M times, then rest')}
              badge={hit('limit')}
            >
              <div className='flex items-center gap-2'>
                {num('rotation_limit_minutes', m)}
                {num('rotation_limit_count', times)}
                {num('rotation_limit_rest_minutes', m)}
                {sw('rotation_limit_enabled')}
              </div>
            </Row>
            <Row
              label={t('Rest when the quota is almost used up')}
              hint={t('Until the quota window resets')}
              badge={hit('quota')}
            >
              <div className='flex items-center gap-2'>
                {num('rotation_quota_percent', '%')}
                {sw('rotation_quota_enabled')}
              </div>
            </Row>
            <Row
              label={t('Most working hours a day')}
              hint={t('Rests until midnight once reached')}
              badge={hit('daily')}
            >
              <div className='flex items-center gap-2'>
                {num('rotation_daily_hours', h)}
                {sw('rotation_daily_enabled')}
              </div>
            </Row>
          </Group>
        </TabsContent>

        <TabsContent value='accounts'>
          <div>
            <div className='flex items-baseline justify-between gap-2'>
              <Label className='font-medium'>{t('Accounts taking part')}</Label>
              <span className='text-muted-foreground text-xs'>
                {t('{{taking}} of {{total}}', {
                  taking,
                  total: props.accounts.length,
                })}
              </span>
            </div>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t(
                'Untick an account to keep it out of rotation (e.g. a test account)'
              )}
            </p>
            <div className='mt-2 space-y-1 rounded-md border p-2'>
              {props.accounts.length === 0 && (
                <div className='text-muted-foreground text-xs'>
                  {t('No Claude accounts')}
                </div>
              )}
              {props.accounts.map((account) => {
                const key = rotationKey(account)
                const email =
                  typeof account.credential?.email === 'string'
                    ? account.credential.email
                    : account.name
                return (
                  <div
                    key={key}
                    className='hover:bg-muted/60 flex items-center gap-2 rounded px-1 py-0.5 text-xs'
                  >
                    <label className='flex min-w-0 flex-1 cursor-pointer items-center gap-2'>
                      <Checkbox
                        checked={!excluded.has(key)}
                        onCheckedChange={(checked) =>
                          toggleAccount(key, checked === true)
                        }
                      />
                      <span className='min-w-0 flex-1 truncate'>{email}</span>
                    </label>
                    <span className='text-muted-foreground flex-none'>
                      {props.instanceNames.get(account.instance_id) ?? '-'}
                    </span>
                    {bools.rotation_shift_enabled && (
                      <NativeSelect
                        size='sm'
                        className='w-24 flex-none'
                        value={shifts.get(key) ?? ''}
                        onChange={(e) =>
                          setShifts((prev) =>
                            new Map(prev).set(key, e.target.value as ShiftKind)
                          )
                        }
                      >
                        {!shifts.has(key) && (
                          <NativeSelectOption value=''>
                            {t('Auto')}
                          </NativeSelectOption>
                        )}
                        {(['day', 'night', 'all'] as ShiftKind[]).map((v) => (
                          <NativeSelectOption key={v} value={v}>
                            {t(SHIFT_LABEL_KEYS[v])}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </Dialog>
  )
}

/** "N accounts meet this rule right now", next to a rule. */
function HitCount(props: { count: number }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'rounded px-1.5 py-0.5 text-[11px] font-normal whitespace-nowrap tabular-nums',
        props.count > 0
          ? 'bg-sky-50 text-sky-800 dark:bg-sky-500/15 dark:text-sky-200'
          : 'bg-muted text-muted-foreground'
      )}
    >
      {t('{{n}} accounts match now', { n: props.count })}
    </span>
  )
}

function Group(props: {
  title: string
  hint?: string
  badge?: React.ReactNode
  right?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className='rounded-lg border'>
      <div className='bg-muted/40 flex items-start justify-between gap-3 border-b px-3 py-2'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-x-2 gap-y-0.5 font-semibold'>
            {props.title}
            {props.badge}
          </div>
          {props.hint && (
            <div className='text-muted-foreground text-xs'>{props.hint}</div>
          )}
        </div>
        {props.right && <div className='flex-none pt-0.5'>{props.right}</div>}
      </div>
      <div className='divide-y px-3'>{props.children}</div>
    </div>
  )
}

function Row(props: {
  label: string
  hint: string
  badge?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className='flex items-center justify-between gap-4 py-2.5'>
      <div className='min-w-0'>
        <div className='flex flex-wrap items-center gap-x-2 gap-y-0.5 font-medium'>
          {props.label}
          {props.badge}
        </div>
        {props.hint && (
          <div className='text-muted-foreground text-xs'>{props.hint}</div>
        )}
      </div>
      <div className='flex-none'>{props.children}</div>
    </div>
  )
}

function NumberInput(props: {
  value: string
  step: number
  suffix: string
  wide?: boolean
  onChange: (v: string) => void
}) {
  return (
    <div className='flex items-center gap-1'>
      <Input
        className={cn('h-8 text-right', props.wide ? 'w-[104px]' : 'w-[68px]')}
        type='number'
        min={0}
        step={props.step}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
      />
      <span className='text-muted-foreground w-9 text-xs'>{props.suffix}</span>
    </div>
  )
}
