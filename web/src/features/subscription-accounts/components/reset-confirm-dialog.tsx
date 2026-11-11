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
import { Loader2, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

type ResetConfirmDialogProps = {
  /** Email (or label) shown to the user. */
  identity: string
  /** Auth file name, shown on hover. */
  fileName: string
  left: number
  direct: boolean
  loading: boolean
  onCancel: () => void
  onConfirm: () => void
}

/**
 * Reset grant confirmation, centered small layout (design draft 2026-11-11,
 * option C): icon, title, account, "left N → N-1 · egress", warning line,
 * and two equal-width buttons. Uses the stock size='sm' alert dialog; the
 * shared ConfirmDialog stays unchanged for enable / disable.
 */
export function ResetConfirmDialog(props: ResetConfirmDialogProps) {
  const { t } = useTranslation()
  const after = Math.max(props.left - 1, 0)
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open) props.onCancel()
      }}
    >
      <AlertDialogContent size='sm'>
        <AlertDialogHeader>
          <AlertDialogMedia className='bg-red-100 text-red-600 dark:bg-red-500/15 dark:text-red-400'>
            <RotateCcw />
          </AlertDialogMedia>
          <AlertDialogTitle>{t('Use one reset grant?')}</AlertDialogTitle>
          <AlertDialogDescription
            render={<div />}
            className='flex w-full min-w-0 flex-col items-center'
          >
            <span
              className='text-foreground block w-full truncate'
              title={`${props.identity} · ${props.fileName}`}
            >
              {props.identity}
            </span>
            <span className='mt-0.5 block tabular-nums'>
              {t('Remaining')} {t('{{count}} times', { count: props.left })}
              {' → '}
              <span className='text-foreground font-medium'>
                {t('{{count}} times', { count: after })}
              </span>
              {' · '}
              {props.direct ? (
                <span className='text-amber-700 dark:text-amber-400'>
                  {t('Direct')}
                </span>
              ) : (
                <span className='text-emerald-700 dark:text-emerald-400'>
                  {t('SOCKS proxy')}
                </span>
              )}
            </span>
            <span className='mt-3 block text-xs'>
              {t("Clears this account's rate limit. This cannot be undone.")}
            </span>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.loading}>
            {t('Cancel')}
          </AlertDialogCancel>
          <Button
            variant='destructive'
            onClick={props.onConfirm}
            disabled={props.loading}
          >
            {props.loading && <Loader2 className='animate-spin' />}
            {t('Continue')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
