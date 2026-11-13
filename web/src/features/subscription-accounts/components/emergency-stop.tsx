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
import { Siren } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import { OVERVIEW_QUERY_KEY, stopRotation } from '../api'

/**
 * Emergency button (safety gate 7, user decision 2026-11-12): rotation is
 * switched off and every account it had sent resting comes back at once.
 * Accounts stopped for safety (SOCKS, 401 / 403, by hand) stay as they are.
 */
export function EmergencyStopButton(props: {
  size?: 'xs' | 'sm'
  onDone?: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const mutation = useMutation({
    mutationFn: stopRotation,
    onSuccess: (data) => {
      setConfirming(false)
      if (data.failed > 0) {
        toast.error(
          t(
            'Rotation is off. {{woken}} accounts are back online, {{failed}} could not be brought back: check them on their cards.',
            { woken: data.woken, failed: data.failed }
          )
        )
      } else {
        toast.success(
          t('Rotation is off. {{woken}} accounts are back online.', {
            woken: data.woken,
          })
        )
      }
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      props.onDone?.()
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <>
      <Button
        size={props.size ?? 'xs'}
        variant='outline'
        className='border-red-300 text-red-700 hover:bg-red-50 hover:text-red-800 dark:border-red-500/50 dark:text-red-300 dark:hover:bg-red-500/15'
        onClick={() => setConfirming(true)}
      >
        <Siren />
        {t('Bring all back and pause')}
      </Button>
      {confirming && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirming(false)
          }}
          title={t('Bring every resting account back now?')}
          desc={t(
            'Rotation is switched off and every account it sent resting is enabled again right away. Accounts stopped for safety (SOCKS, 401 / 403) or by hand are not touched. Switch rotation on again in the settings when you are ready.'
          )}
          confirmText={t('Bring all back and pause')}
          destructive
          isLoading={mutation.isPending}
          handleConfirm={() => mutation.mutate()}
        />
      )}
    </>
  )
}
