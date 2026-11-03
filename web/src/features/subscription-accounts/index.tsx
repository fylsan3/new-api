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
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  OVERVIEW_QUERY_KEY,
  deleteInstance,
  getOverview,
  syncInstances,
} from './api'
import { AccountCard } from './components/account-card'
import { InstanceDialog } from './components/instance-dialog'
import { RefreshControls } from './components/refresh-controls'
import { sortAccounts } from './lib/quota'
import type { CpaInstance } from './types'

export function SubscriptionAccounts() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialog, setDialog] = useState<{ instance?: CpaInstance } | null>(null)
  const [deleting, setDeleting] = useState<CpaInstance | null>(null)

  const query = useQuery({
    queryKey: OVERVIEW_QUERY_KEY,
    queryFn: getOverview,
    refetchInterval: (q) => (q.state.data?.refresh_all_running ? 3000 : 30000),
  })

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
      <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
        {accounts.map((account) => (
          <AccountCard
            key={`${account.instance_id}/${account.name}`}
            account={account}
            instanceName={instanceNames.get(account.instance_id) ?? '-'}
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
              <RefreshControls
                key={`${data.settings.refresh_all_cooldown_seconds}-${data.settings.auto_refresh_interval_minutes}`}
                settings={data.settings}
                running={data.refresh_all_running}
                serverNow={data.now}
              />
            )}
          </div>

          {data?.instances.length ? (
            <div className='flex flex-wrap gap-2'>
              {data.instances.map((instance) => (
                <div
                  key={instance.id}
                  className='flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm'
                >
                  <span
                    className={
                      instance.last_test_ok
                        ? 'size-2 rounded-full bg-emerald-500'
                        : 'bg-muted-foreground/40 size-2 rounded-full'
                    }
                  />
                  <span className='font-medium'>{instance.name}</span>
                  <span className='text-muted-foreground text-xs'>
                    {instance.base_url}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {t('Key')}: {instance.has_secret ? '****' : '-'}
                  </span>
                  {instance.last_test_at > 0 && (
                    <span
                      className='text-muted-foreground text-xs'
                      title={instance.last_test_msg}
                    >
                      {t('Tested')}{' '}
                      {formatTimestampToDate(instance.last_test_at)}
                    </span>
                  )}
                  <Button
                    size='icon-xs'
                    variant='ghost'
                    aria-label={t('Edit')}
                    onClick={() => setDialog({ instance })}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    size='icon-xs'
                    variant='ghost'
                    aria-label={t('Delete')}
                    onClick={() => setDeleting(instance)}
                  >
                    <Trash2 />
                  </Button>
                </div>
              ))}
            </div>
          ) : null}

          {content}
        </div>

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
