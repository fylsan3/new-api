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
  Activity,
  ArrowDownUp,
  BellRing,
  Gauge,
  Loader2,
  Send,
} from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import { OVERVIEW_QUERY_KEY, testAlertChannel, updateSettings } from '../api'
import { accountDisplayName } from '../lib/pool'
import { rateOf, rateText } from '../lib/rotation'
import type {
  CpaAccount,
  CpaRate,
  CpaSettings,
  CpaSettingsInput,
  SubscriptionOverview,
} from '../types'
import { RateChart } from './rate-chart'
import { Section } from './stats-panel'

type Channel = 'wecom' | 'dingtalk'

type EventKey =
  | 'alert_on_socks'
  | 'alert_on_auth'
  | 'alert_on_cpa'
  | 'alert_on_rotation'

/** "Monitoring & alerts" tab of the subscription accounts page. */
export function MonitorPanel(props: { data: SubscriptionOverview }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <Section
        icon={<Activity />}
        title={t('Pool request rate')}
        description={t(
          'All accounts together. Bars are the busiest minute of each period, the line is its per-minute average; the last hour is drawn minute by minute. Failed requests are red, 429s are left out.'
        )}
      >
        <div className='p-3'>
          <RateChart height={260} />
        </div>
      </Section>
      <RateTable data={props.data} />
      <AlertSettings
        key={settingsKey(props.data.settings)}
        settings={props.data.settings}
      />
    </div>
  )
}

/** Re-mounts the form when the saved values change elsewhere. */
function settingsKey(s: CpaSettings) {
  return [
    s.auth_stop_enabled,
    s.alert_wecom_enabled,
    s.alert_dingtalk_enabled,
    s.alert_on_socks,
    s.alert_on_auth,
    s.alert_on_cpa,
    s.alert_on_rotation,
    s.has_wecom_webhook,
    s.has_dingtalk_webhook,
    s.has_dingtalk_secret,
  ].join('|')
}

type RateSortKey = 'account' | 'cpa' | keyof CpaRate

const RATE_COLUMNS: { key: keyof CpaRate; labelKey: string }[] = [
  { key: 'rpm_1m', labelKey: 'RPM' },
  { key: 'tpm_1m', labelKey: 'TPM' },
  { key: 'rpm_5m', labelKey: 'RPM 5 min avg' },
  { key: 'tpm_5m', labelKey: 'TPM 5 min avg' },
  { key: 'rpm_peak_1h', labelKey: 'Peak RPM, last hour (no 429)' },
  { key: 'tpm_peak_1h', labelKey: 'Peak TPM, last hour' },
  { key: 'r429_1h', labelKey: '429 in the last hour' },
]

/**
 * Per-account RPM / TPM. Click a header to sort (busiest first by
 * default); the last row is the whole pool.
 */
function RateTable(props: { data: SubscriptionOverview }) {
  const { t } = useTranslation()
  const [sort, setSort] = useState<{ key: RateSortKey; desc: boolean }>({
    key: 'rpm_5m',
    desc: true,
  })
  const names = new Map(props.data.instances.map((i) => [i.id, i.name]))
  const rows = props.data.accounts
    .map((account: CpaAccount) => ({
      account,
      rate: rateOf(props.data.rates, account),
    }))
    .filter((row) => row.account.is_claude || row.rate)
    .sort((a, b) => {
      // Accounts without data stay at the bottom either way.
      if (!a.rate !== !b.rate) return a.rate ? -1 : 1
      const dir = sort.desc ? -1 : 1
      if (sort.key === 'account') {
        return (
          accountDisplayName(a.account).localeCompare(
            accountDisplayName(b.account)
          ) * dir
        )
      }
      if (sort.key === 'cpa') {
        return (
          (names.get(a.account.instance_id) ?? '').localeCompare(
            names.get(b.account.instance_id) ?? ''
          ) * dir
        )
      }
      const key = sort.key
      return (
        ((a.rate?.[key] ?? 0) - (b.rate?.[key] ?? 0)) * dir ||
        (b.rate?.tpm_5m ?? 0) - (a.rate?.tpm_5m ?? 0)
      )
    })
  const total = props.data.rate_total
  const withData = rows.filter((row) => row.rate).length

  const header = (key: RateSortKey, label: string, align = 'text-right') => (
    <TableHead className={align}>
      <button
        type='button'
        className='hover:text-foreground inline-flex items-center gap-1'
        onClick={() =>
          setSort((prev) =>
            prev.key === key
              ? { key, desc: !prev.desc }
              : { key, desc: key !== 'account' && key !== 'cpa' }
          )
        }
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

  return (
    <Section
      icon={<Gauge />}
      title={t('Account request rate')}
      description={t(
        'Click a column header to sort. Peaks leave out 429s; the pool peak is the busiest minute of all accounts together. TPM counts input and output tokens, without cache, the same way as the usage log.'
      )}
    >
      <Table>
        <TableHeader>
          <TableRow>
            {header('account', t('Account'), 'text-left')}
            {header('cpa', 'CPA', 'text-left')}
            {RATE_COLUMNS.map((column) => (
              <Fragment key={column.key}>
                {header(column.key, t(column.labelKey))}
              </Fragment>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 && (
            <TableRow>
              <TableCell
                colSpan={RATE_COLUMNS.length + 2}
                className='text-muted-foreground text-center text-xs'
              >
                {t('No data')}
              </TableCell>
            </TableRow>
          )}
          {rows.map(({ account, rate }) => (
            <TableRow key={`${account.instance_id}/${account.name}`}>
              <TableCell className='max-w-56 truncate'>
                {accountDisplayName(account)}
              </TableCell>
              <TableCell className='text-muted-foreground text-xs'>
                {names.get(account.instance_id) ?? '-'}
              </TableCell>
              <RateCells rate={rate} />
            </TableRow>
          ))}
        </TableBody>
        {total && rows.length > 0 && (
          <TableFooter>
            <TableRow>
              <TableCell className='font-medium'>{t('Pool total')}</TableCell>
              <TableCell className='text-muted-foreground text-xs'>
                {t('{{n}} accounts with requests', { n: withData })}
              </TableCell>
              <RateCells rate={total} />
            </TableRow>
          </TableFooter>
        )}
      </Table>
    </Section>
  )
}

/** The number cells of one rate row. */
function RateCells(props: { rate: CpaRate | null }) {
  const rate = props.rate
  return (
    <>
      {RATE_COLUMNS.map((column) => (
        <TableCell
          key={column.key}
          className={cn(
            'text-right tabular-nums',
            column.key === 'r429_1h' &&
              (rate?.r429_1h ?? 0) > 0 &&
              'text-red-600 dark:text-red-400'
          )}
        >
          {rate ? rateText(rate[column.key] ?? 0) : '-'}
        </TableCell>
      ))}
    </>
  )
}

/** Webhook channels, pushed events and the 401 / 403 stop. */
function AlertSettings(props: { settings: CpaSettings }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const s = props.settings
  const [form, setForm] = useState({
    auth_stop_enabled: s.auth_stop_enabled,
    alert_wecom_enabled: s.alert_wecom_enabled,
    alert_dingtalk_enabled: s.alert_dingtalk_enabled,
    alert_on_socks: s.alert_on_socks,
    alert_on_auth: s.alert_on_auth,
    alert_on_cpa: s.alert_on_cpa,
    alert_on_rotation: s.alert_on_rotation,
  })
  // Secrets are write-only: empty keeps the saved value.
  const [wecomHook, setWecomHook] = useState('')
  const [dingHook, setDingHook] = useState('')
  const [dingSecret, setDingSecret] = useState('')

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })

  const saveMutation = useMutation({
    mutationFn: (input: CpaSettingsInput) => updateSettings(input),
    onSuccess: () => {
      toast.success(t('Saved'))
      setWecomHook('')
      setDingHook('')
      setDingSecret('')
      invalidate()
    },
    onError: (error) => handleServerError(error),
  })

  const testMutation = useMutation({
    mutationFn: (channel: Channel) => testAlertChannel(channel),
    onSuccess: () => toast.success(t('Test message sent')),
    onError: (error) => handleServerError(error),
  })

  const save = () =>
    saveMutation.mutate({
      ...form,
      alert_wecom_webhook: wecomHook.trim(),
      alert_dingtalk_webhook: dingHook.trim(),
      alert_dingtalk_secret: dingSecret.trim(),
    })

  const clear = (field: keyof CpaSettingsInput) =>
    saveMutation.mutate({ [field]: '-' })

  const events: { key: EventKey; label: string }[] = [
    { key: 'alert_on_socks', label: t('SOCKS unreachable, account disabled') },
    { key: 'alert_on_auth', label: t('401 / 403, account disabled') },
    {
      key: 'alert_on_cpa',
      label: t('CPA unreachable or usage collection error'),
    },
    {
      key: 'alert_on_rotation',
      label: t('Rotation / shift took accounts on or off'),
    },
  ]

  const dirty =
    wecomHook.trim() !== '' ||
    dingHook.trim() !== '' ||
    dingSecret.trim() !== '' ||
    (Object.keys(form) as (keyof typeof form)[]).some(
      (key) => form[key] !== s[key]
    )

  return (
    <Section
      icon={<BellRing />}
      title={t('Alerts')}
      description={t(
        'Messages are sent from the New API server; it must be able to reach the WeCom / DingTalk address.'
      )}
      actions={
        <Button
          size='sm'
          disabled={!dirty || saveMutation.isPending}
          onClick={save}
        >
          {saveMutation.isPending && <Loader2 className='animate-spin' />}
          {t('Save')}
        </Button>
      }
    >
      <div className='divide-y px-3 text-sm sm:px-5'>
        <Row
          label={t('Disable an account on 401 / 403')}
          hint={t(
            'The sign-in is broken; rotation never brings it back. Re-enable it by hand once fixed.'
          )}
        >
          <Switch
            checked={form.auth_stop_enabled}
            onCheckedChange={(v) =>
              setForm((f) => ({ ...f, auth_stop_enabled: v }))
            }
          />
        </Row>

        <ChannelBlock
          title={t('WeCom group bot')}
          enabled={form.alert_wecom_enabled}
          onEnabled={(v) => setForm((f) => ({ ...f, alert_wecom_enabled: v }))}
          saved={s.has_wecom_webhook}
          testing={testMutation.isPending && testMutation.variables === 'wecom'}
          onTest={() => testMutation.mutate('wecom')}
          onClear={() => clear('alert_wecom_webhook')}
        >
          <Input
            className='h-8'
            placeholder={
              s.has_wecom_webhook
                ? t('Saved; leave empty to keep it')
                : 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...'
            }
            value={wecomHook}
            onChange={(e) => setWecomHook(e.target.value)}
          />
        </ChannelBlock>

        <ChannelBlock
          title={t('DingTalk group bot')}
          enabled={form.alert_dingtalk_enabled}
          onEnabled={(v) =>
            setForm((f) => ({ ...f, alert_dingtalk_enabled: v }))
          }
          saved={s.has_dingtalk_webhook}
          testing={
            testMutation.isPending && testMutation.variables === 'dingtalk'
          }
          onTest={() => testMutation.mutate('dingtalk')}
          onClear={() => clear('alert_dingtalk_webhook')}
        >
          <Input
            className='h-8'
            placeholder={
              s.has_dingtalk_webhook
                ? t('Saved; leave empty to keep it')
                : 'https://oapi.dingtalk.com/robot/send?access_token=...'
            }
            value={dingHook}
            onChange={(e) => setDingHook(e.target.value)}
          />
          <div className='mt-1.5 flex items-center gap-2'>
            <Input
              className='h-8'
              type='password'
              autoComplete='off'
              placeholder={
                s.has_dingtalk_secret
                  ? t('Signing secret saved; leave empty to keep it')
                  : t('Signing secret (SEC...), optional')
              }
              value={dingSecret}
              onChange={(e) => setDingSecret(e.target.value)}
            />
            {s.has_dingtalk_secret && (
              <Button
                size='xs'
                variant='ghost'
                onClick={() => clear('alert_dingtalk_secret')}
              >
                {t('Clear')}
              </Button>
            )}
          </div>
        </ChannelBlock>

        <div className='py-3'>
          <div className='font-medium'>{t('Events to push')}</div>
          <div className='text-muted-foreground text-xs'>
            {t(
              'The same event for the same account is sent at most once every 5 minutes'
            )}
          </div>
          <div className='mt-2 grid gap-2 sm:grid-cols-2'>
            {events.map((event) => (
              <label
                key={event.key}
                className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'
              >
                <span className='text-xs'>{event.label}</span>
                <Switch
                  checked={form[event.key]}
                  onCheckedChange={(v) =>
                    setForm((f) => ({ ...f, [event.key]: v }))
                  }
                />
              </label>
            ))}
          </div>
        </div>
      </div>
    </Section>
  )
}

function ChannelBlock(props: {
  title: string
  enabled: boolean
  onEnabled: (v: boolean) => void
  saved: boolean
  testing: boolean
  onTest: () => void
  onClear: () => void
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className='py-3'>
      <div className='flex items-center justify-between gap-3'>
        <div className='flex items-center gap-2'>
          <span className='font-medium'>{props.title}</span>
          <Badge variant={props.saved ? 'secondary' : 'outline'}>
            {props.saved ? t('Address saved') : t('Not set')}
          </Badge>
        </div>
        <div className='flex items-center gap-2'>
          {props.saved && (
            <>
              <Button
                size='xs'
                variant='outline'
                disabled={props.testing}
                onClick={props.onTest}
              >
                {props.testing ? (
                  <Loader2 className='animate-spin' />
                ) : (
                  <Send />
                )}
                {t('Send test')}
              </Button>
              <Button size='xs' variant='ghost' onClick={props.onClear}>
                {t('Clear')}
              </Button>
            </>
          )}
          <Switch checked={props.enabled} onCheckedChange={props.onEnabled} />
        </div>
      </div>
      <div className='mt-2'>{props.children}</div>
    </div>
  )
}

function Row(props: {
  label: string
  hint: string
  children: React.ReactNode
}) {
  return (
    <div className='flex items-center justify-between gap-4 py-3'>
      <div className='min-w-0'>
        <div className='font-medium'>{props.label}</div>
        <div className='text-muted-foreground text-xs'>{props.hint}</div>
      </div>
      <div className='flex-none'>{props.children}</div>
    </div>
  )
}
