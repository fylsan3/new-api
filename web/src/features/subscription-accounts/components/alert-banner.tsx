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
import { AlertTriangle, Check, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { ALERTS_QUERY_KEY, OVERVIEW_QUERY_KEY, acknowledgeAlerts } from '../api'
import type { CpaAlert } from '../types'

const KIND_LABEL_KEYS: Record<string, string> = {
  socks_auto_disabled: 'SOCKS failed, account disabled',
  socks_auto_disable_failed: 'SOCKS failed, auto-disable also failed',
  usage_statistics_off: 'CPA usage statistics turned off',
}

/** Open subscription-account alerts. They stay until acknowledged. */
export function CpaAlertBanner(props: { alerts: CpaAlert[] }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const ackMutation = useMutation({
    mutationFn: (ids?: number[]) => acknowledgeAlerts(ids),
    onSuccess: () => {
      toast.success(t('Acknowledged'))
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: ALERTS_QUERY_KEY })
    },
    onError: (error) => handleServerError(error),
  })

  if (!props.alerts.length) return null

  return (
    <div className='space-y-1.5 rounded-lg border border-red-300 bg-red-50 p-3 text-sm text-red-900 dark:border-red-500/40 dark:bg-red-500/10 dark:text-red-200'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-1.5 font-medium'>
          <AlertTriangle className='size-4' />
          {t('{{count}} subscription account alert(s) need your confirmation', {
            count: props.alerts.length,
          })}
        </div>
        {props.alerts.length > 1 && (
          <Button
            size='xs'
            variant='outline'
            disabled={ackMutation.isPending}
            onClick={() => ackMutation.mutate(undefined)}
          >
            {ackMutation.isPending ? (
              <Loader2 className='animate-spin' />
            ) : (
              <Check />
            )}
            {t('Acknowledge all')}
          </Button>
        )}
      </div>
      <ul className='space-y-1'>
        {props.alerts.map((alert) => (
          <li
            key={alert.id}
            className='flex items-start justify-between gap-2 text-xs'
          >
            <span className='min-w-0 break-all'>
              <span className='font-medium'>{alert.name || '-'}</span>
              {' · '}
              {t(KIND_LABEL_KEYS[alert.kind] ?? alert.kind)}
              {alert.count > 1 ? ` ×${alert.count}` : ''}
              {' · '}
              {formatTimestampToDate(alert.last_seen_at)}
              <span className='block opacity-80'>{alert.message}</span>
            </span>
            <Button
              size='xs'
              variant='outline'
              className='shrink-0'
              disabled={ackMutation.isPending}
              onClick={() => ackMutation.mutate([alert.id])}
            >
              {t('Got it')}
            </Button>
          </li>
        ))}
      </ul>
    </div>
  )
}
