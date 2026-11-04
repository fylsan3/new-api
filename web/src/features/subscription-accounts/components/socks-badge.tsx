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
import { useMutation } from '@tanstack/react-query'
import { Copy, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import { type AccountRef, revealAccountProxy } from '../api'
import { type SocksState, socksStateOf } from '../lib/card'
import type { CpaAccount } from '../types'

const STATE_CLASS: Record<SocksState, { badge: string; dot: string }> = {
  ok: {
    badge:
      'border-emerald-400 bg-emerald-50 text-emerald-900 dark:border-emerald-500/50 dark:bg-emerald-500/15 dark:text-emerald-200',
    dot: 'bg-emerald-500',
  },
  failed: {
    badge:
      'border-red-300 bg-red-50 text-red-900 dark:border-red-500/50 dark:bg-red-500/15 dark:text-red-200',
    dot: 'bg-red-500',
  },
  missing: {
    badge: 'border-border bg-muted text-muted-foreground',
    dot: 'bg-muted-foreground/50',
  },
}

/**
 * SOCKS status from the last check through CPA. No hover popup (the masked
 * address is already in the card's hover details); clicking fetches the full
 * address (with credentials) on demand.
 */
export function SocksBadge(props: {
  account: CpaAccount
  accountRef: AccountRef
}) {
  const { t } = useTranslation()
  const [full, setFull] = useState<string | null>(null)
  const [pinned, setPinned] = useState(false)
  const state = socksStateOf(props.account)
  const masked = props.account.proxy_url || t('Not configured')

  const revealMutation = useMutation({
    mutationFn: () => revealAccountProxy(props.accountRef),
    onSuccess: (data) => setFull(data.proxy_url || ''),
    onError: (error) => handleServerError(error),
  })

  const stateText = {
    ok: t('SOCKS reachable at last check'),
    failed: t('SOCKS unreachable at last check'),
    missing: t('No socks5 proxy configured, quota checks are blocked'),
  }[state]

  return (
    <Popover
      onOpenChange={(open, details) => {
        if (open && details.reason === 'trigger-press') {
          setPinned(true)
          if (full === null && !revealMutation.isPending) {
            revealMutation.mutate()
          }
        }
        if (!open) {
          setPinned(false)
          setFull(null)
        }
      }}
    >
      <PopoverTrigger
        aria-label={t('SOCKS proxy')}
        render={
          <button
            type='button'
            className={cn(
              'inline-flex h-4 shrink-0 cursor-pointer items-center gap-1 rounded-md border px-1.5 font-mono text-[11px] leading-none',
              STATE_CLASS[state].badge
            )}
          />
        }
      >
        <span className={cn('size-1.5 rounded-full', STATE_CLASS[state].dot)} />
        SOCKS
      </PopoverTrigger>
      <PopoverContent align='end' className='w-72 space-y-1.5 text-xs'>
        <div className='font-medium'>{stateText}</div>
        {props.account.proxy_error && state !== 'ok' && (
          <div className='break-all text-red-600 dark:text-red-400'>
            {props.account.proxy_error}
          </div>
        )}
        <div className='bg-muted rounded px-2 py-1 font-mono break-all'>
          {pinned && full !== null ? full || t('Not configured') : masked}
        </div>
        {pinned && revealMutation.isPending && (
          <div className='text-muted-foreground flex items-center gap-1'>
            <Loader2 className='size-3 animate-spin' /> {t('Loading')}
          </div>
        )}
        {pinned && full ? (
          <Button
            size='xs'
            variant='outline'
            onClick={async () => {
              if (await copyToClipboard(full)) toast.success(t('Copied'))
            }}
          >
            <Copy /> {t('Copy')}
          </Button>
        ) : (
          !pinned && (
            <div className='text-muted-foreground'>
              {t('Click to show the full address with username and password')}
            </div>
          )
        )}
      </PopoverContent>
    </Popover>
  )
}
