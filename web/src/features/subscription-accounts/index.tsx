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
import {
  Loader2,
  Pencil,
  Plus,
  RefreshCcw,
  ServerCog,
  Settings2,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  OVERVIEW_QUERY_KEY,
  STATS_QUERY_KEY,
  deleteInstance,
  getOverview,
  getStats,
  syncInstances,
} from './api'
import { AccountCard } from './components/account-card'
import { CpaAlertBanner } from './components/alert-banner'
import { InstanceDialog } from './components/instance-dialog'
import { RefreshControls } from './components/refresh-controls'
import { RotationBar } from './components/rotation-bar'
import { RotationSettingsDialog } from './components/rotation-settings-dialog'
import { alertsOf, cardUsageOf } from './lib/card'
import { sortAccounts } from './lib/quota'
import { rateOf, rotationOf } from './lib/rotation'
import type { CpaAccountStats, CpaInstance } from './types'

/** Window of the per-account token breakdown shown in card details. */
const CARD_STATS_DAYS = 30

export function SubscriptionAccounts() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialog, setDialog] = useState<{ instance?: CpaInstance } | null>(null)
  const [deleting, setDeleting] = useState<CpaInstance | null>(null)
  const [rotationOpen, setRotationOpen] = useState(false)

  const query = useQuery({
    queryKey: OVERVIEW_QUERY_KEY,
    queryFn: getOverview,
    // Reads only New API's own cache (no CPA / Anthropic call). 5s per user
    // decision 2026-11-06; react-query pauses it while the tab is hidden and
    // refetches as soon as the tab is focused again.
    refetchInterval: (q) => (q.state.data?.refresh_all_running ? 3000 : 5000),
    refetchOnWindowFocus: true,
  })

  // Token breakdown for the hover details; one request for all cards.
  const statsQuery = useQuery({
    queryKey: [...STATS_QUERY_KEY, CARD_STATS_DAYS],
    queryFn: () => getStats(CARD_STATS_DAYS),
    enabled: !!query.data?.instances.length,
    staleTime: 60_000,
  })
  const statsByAccount = new Map<string, CpaAccountStats>(
    (statsQuery.data?.accounts ?? []).map((row) => [
      `${row.instance_id}/${row.name}`,
      row,
    ])
  )

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })

  const syncMutation = useMutation({
    mutationFn: syncInstances,
    onSuccess: (data) => {
      const failures = Object.values(data.failures)
      if (failures.length) {
        toast.error(failures.join('\n'))
      } else {
        toast.success(t('Account list synced'))
      }
      invalidate()
    },
    onError: (error) => handleServerError(error),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteInstance(id),
    onSuccess: () => {
      setDeleting(null)
      toast.success(t('Deleted'))
      invalidate()
    },
    onError: (error) => handleServerError(error),
  })

  const data = query.data
  const instanceNames = new Map(
    (data?.instances ?? []).map((instance) => [instance.id, instance.name])
  )
  const accounts = sortAccounts(data?.accounts ?? [])
  const collecting = new Set(
    (data?.instances ?? [])
      .filter((instance) => instance.usage_collect_enabled)
      .map((instance) => instance.id)
  )

  let content: React.ReactNode
  if (query.isLoading) {
    content = <LoadingState />
  } else if (query.isError) {
    content = (
      <ErrorState
        description={getServerErrorMessage(query.error)}
        onRetry={() => query.refetch()}
      />
    )
  } else if (!data?.instances.length) {
    content = (
      <EmptyState
        icon={ServerCog}
        title={t('No CPA added yet')}
        description={t(
          'Add a CPA remote management address to see its subscription accounts.'
        )}
        action={
          <Button onClick={() => setDialog({})}>
            <Plus /> {t('Add CPA subscription info')}
          </Button>
        }
      />
    )
  } else if (!accounts.length) {
    content = (
      <EmptyState
        title={t('No accounts cached')}
        description={t(
          'Click "Sync accounts" to load the account list from CPA.'
        )}
      />
    )
  } else {
    content = (
      // Grid rows of 1fr: every card is as tall as the tallest one, and that
      // height still follows its content (user request 2026-11-12).
      <div className='grid auto-rows-fr grid-cols-[repeat(auto-fill,300px)] gap-4'>
        {accounts.map((account) => (
          <AccountCard
            key={`${account.instance_id}/${account.name}`}
            account={account}
            instanceName={instanceNames.get(account.instance_id) ?? '-'}
            usage={cardUsageOf(data.usage, account)}
            stats={
              statsByAccount.get(`${account.instance_id}/${account.name}`) ??
              null
            }
            collecting={collecting.has(account.instance_id)}
            alerts={alertsOf(data.alerts, account)}
            rotation={rotationOf(data.rotation, account)}
            rotationEnabled={data.settings.rotation_enabled}
            rotationBadges={data.settings.rotation_show_badges}
            rate={rateOf(data.rates, account)}
          />
        ))}
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>{t('Subscription Accounts')}</span>
          <Badge variant='outline' className='shrink-0'>
            Root
          </Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <div className='flex flex-wrap items-center gap-2'>
              <Button size='sm' variant='outline' onClick={() => setDialog({})}>
                <Plus /> {t('Add CPA subscription info')}
              </Button>
              <Button
                size='sm'
                variant='outline'
                disabled={syncMutation.isPending || !data?.instances.length}
                onClick={() => syncMutation.mutate()}
              >
                {syncMutation.isPending ? (
                  <Loader2 className='animate-spin' />
                ) : (
                  <RefreshCcw />
                )}
                {t('Sync accounts')}
              </Button>
            </div>
            {data && (
              <div className='flex flex-wrap items-center gap-3'>
                {data.instances.length > 0 &&
                  !data.settings.rotation_show_badges && (
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => setRotationOpen(true)}
                    >
                      <Settings2 />
                      {t('Rotation settings')}
                    </Button>
                  )}
                <RefreshControls
                  key={data.settings.refresh_all_cooldown_seconds}
                  settings={data.settings}
                  running={data.refresh_all_running}
                  serverNow={data.now}
                />
              </div>
            )}
          </div>

          {data?.alerts?.length ? (
            <CpaAlertBanner alerts={data.alerts} />
          ) : null}

          {data?.instances.length ? (
            <div className='flex flex-wrap gap-2'>
              {data.instances.map((instance) => (
                <InstancePill
                  key={instance.id}
                  instance={instance}
                  onEdit={() => setDialog({ instance })}
                  onDelete={() => setDeleting(instance)}
                />
              ))}
            </div>
          ) : null}

          {data?.instances.length && data.settings.rotation_show_badges ? (
            <RotationBar
              summary={data.rotation}
              enabled={data.settings.rotation_enabled}
              onSettings={() => setRotationOpen(true)}
            />
          ) : null}

          {content}
        </div>

        {rotationOpen && data && (
          <RotationSettingsDialog
            open
            onOpenChange={setRotationOpen}
            settings={data.settings}
            accounts={accounts.filter((account) => account.is_claude)}
            instanceNames={instanceNames}
            rotation={data.rotation}
          />
        )}

        {dialog && (
          <InstanceDialog
            key={dialog.instance?.id ?? 'new'}
            open
            onOpenChange={(open) => {
              if (!open) setDialog(null)
            }}
            instance={dialog.instance}
          />
        )}
        {deleting && (
          <ConfirmDialog
            open
            onOpenChange={(open) => {
              if (!open) setDeleting(null)
            }}
            title={t('Remove this CPA?')}
            desc={t(
              'Only removes {{name}} and its cached accounts from this page. Nothing is deleted on CPA.',
              { name: deleting.name }
            )}
            destructive
            isLoading={deleteMutation.isPending}
            handleConfirm={() => deleteMutation.mutate(deleting.id)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

/** CPA chip: only the name; hovering shows address, key and collection. */
function InstancePill(props: {
  instance: CpaInstance
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const instance = props.instance
  const collectBad =
    instance.usage_collect_enabled &&
    (!!instance.usage_last_error || instance.usage_remote_state === 2)
  return (
    <HoverCard>
      <HoverCardTrigger
        delay={200}
        closeDelay={150}
        render={
          <button
            type='button'
            className='hover:bg-muted/60 flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm'
          />
        }
      >
        <span
          className={
            instance.last_test_ok
              ? 'size-2 rounded-full bg-emerald-500'
              : 'bg-muted-foreground/40 size-2 rounded-full'
          }
        />
        <span className='font-medium'>{instance.name}</span>
        {collectBad && <span className='size-1.5 rounded-full bg-red-500' />}
      </HoverCardTrigger>
      <HoverCardContent align='start' className='w-80 space-y-1.5 text-xs'>
        <div className='text-sm font-medium'>{instance.name}</div>
        <InfoRow label={t('Address')} value={instance.base_url} />
        <InfoRow label={t('Key')} value={instance.has_secret ? '****' : '-'} />
        <InfoRow
          label={t('Usage collection')}
          value={usageStateLabel(instance, t)}
          danger={collectBad}
        />
        {instance.usage_last_error && (
          <div className='break-all text-red-600 dark:text-red-400'>
            {instance.usage_last_error}
          </div>
        )}
        {instance.usage_last_poll_at > 0 && (
          <InfoRow
            label={t('Last poll')}
            value={formatTimestampToDate(instance.usage_last_poll_at)}
          />
        )}
        {instance.last_test_at > 0 && (
          <InfoRow
            label={t('Tested')}
            value={`${formatTimestampToDate(instance.last_test_at)}${
              instance.last_test_msg ? ` · ${instance.last_test_msg}` : ''
            }`}
          />
        )}
        <div className='flex justify-end gap-1.5 border-t pt-2'>
          <Button size='xs' variant='outline' onClick={props.onEdit}>
            <Pencil />
            {t('Edit')}
          </Button>
          <Button size='xs' variant='outline' onClick={props.onDelete}>
            <Trash2 />
            {t('Delete')}
          </Button>
        </div>
      </HoverCardContent>
    </HoverCard>
  )
}

function InfoRow(props: { label: string; value: string; danger?: boolean }) {
  return (
    <div className='flex gap-2'>
      <span className='text-muted-foreground w-16 flex-none'>
        {props.label}
      </span>
      <span
        className={
          props.danger
            ? 'min-w-0 break-all text-red-600 dark:text-red-400'
            : 'min-w-0 break-all'
        }
      >
        {props.value}
      </span>
    </div>
  )
}

// Literal t() calls keep the keys visible to the i18n extractor.
function usageStateLabel(
  instance: CpaInstance,
  t: (key: string) => string
): string {
  if (!instance.usage_collect_enabled) return t('Usage collection off')
  if (instance.usage_remote_state === 2) return t('CPA usage statistics off')
  if (instance.usage_last_error) return t('Usage collection error')
  return t('Collecting usage')
}
