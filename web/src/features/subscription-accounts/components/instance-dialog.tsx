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
import { ChartLine, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  OVERVIEW_QUERY_KEY,
  createInstance,
  testInstance,
  updateInstance,
} from '../api'
import type { CpaInstance } from '../types'

// 「采集用量」开关按状态着色：关=橙黄提醒，开=绿色
const COLLECT_OFF = {
  box: 'border-[#EF9F27] bg-[#FAEEDA] dark:border-[#BA7517] dark:bg-[#412402]',
  text: 'text-[#854F0B] dark:text-[#FAC775]',
  pill: 'bg-[#FAC775] text-[#633806] dark:bg-[#633806] dark:text-[#FAC775]',
}
const COLLECT_ON = {
  box: 'border-[#639922] bg-[#EAF3DE] dark:border-[#3B6D11] dark:bg-[#173404]',
  text: 'text-[#3B6D11] dark:text-[#C0DD97]',
  pill: 'bg-[#C0DD97] text-[#27500A] dark:bg-[#27500A] dark:text-[#C0DD97]',
}
const COLLECT_SWITCH = 'data-checked:bg-[#639922]'

type InstanceDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  instance?: CpaInstance
}

export function InstanceDialog(props: InstanceDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const editing = props.instance
  const [name, setName] = useState(editing?.name ?? '')
  const [baseUrl, setBaseUrl] = useState(editing?.base_url ?? '')
  const [secret, setSecret] = useState(editing?.has_secret ? '****' : '')
  const [collect, setCollect] = useState(
    editing?.usage_collect_enabled ?? false
  )
  const [channelIds, setChannelIds] = useState(editing?.channel_ids ?? '')

  const input = { name, base_url: baseUrl, secret }
  const saveInput = {
    ...input,
    usage_collect_enabled: collect,
    channel_ids: channelIds,
  }

  const testMutation = useMutation({
    mutationFn: () => testInstance({ ...input, id: editing?.id }),
    onSuccess: (data) =>
      toast.success(
        t('Connected. {{count}} credentials found.', {
          count: data.credential_count,
        })
      ),
    onError: (error) => handleServerError(error),
  })

  const saveMutation = useMutation({
    mutationFn: () =>
      editing
        ? updateInstance(editing.id, saveInput)
        : createInstance(saveInput),
    onSuccess: () => {
      toast.success(t('Saved'))
      queryClient.invalidateQueries({ queryKey: OVERVIEW_QUERY_KEY })
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })

  const busy = testMutation.isPending || saveMutation.isPending

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={editing ? t('Edit CPA') : t('Add CPA subscription info')}
      description={t(
        'The management key is stored encrypted and never shown again. Leave **** to keep it.'
      )}
      footer={
        <div className='flex w-full justify-between gap-2'>
          <Button
            variant='outline'
            disabled={busy || !baseUrl.trim()}
            onClick={() => testMutation.mutate()}
          >
            {testMutation.isPending && <Loader2 className='animate-spin' />}
            {t('Test connection')}
          </Button>
          <Button
            disabled={busy || !name.trim() || !baseUrl.trim() || !secret.trim()}
            onClick={() => saveMutation.mutate()}
          >
            {saveMutation.isPending && <Loader2 className='animate-spin' />}
            {t('Save')}
          </Button>
        </div>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-1.5'>
          <Label htmlFor='cpa-name'>{t('Name')}</Label>
          <Input
            id='cpa-name'
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t('e.g. Japan CPA')}
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='cpa-url'>{t('Remote management address')}</Label>
          <Input
            id='cpa-url'
            value={baseUrl}
            onChange={(event) => setBaseUrl(event.target.value)}
            placeholder='http://127.0.0.1:8317'
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='cpa-secret'>{t('Management key')}</Label>
          <PasswordInput
            id='cpa-secret'
            value={secret}
            autoComplete='new-password'
            onChange={(event) => setSecret(event.target.value)}
          />
        </div>
        <div
          className={cn(
            'rounded-lg border-[1.5px] p-3 transition-colors',
            collect ? COLLECT_ON.box : COLLECT_OFF.box
          )}
        >
          <div className='flex items-center gap-2'>
            <ChartLine
              className={cn(
                'size-[18px] shrink-0',
                collect ? COLLECT_ON.text : COLLECT_OFF.text
              )}
            />
            <Label htmlFor='cpa-collect' className='text-sm font-medium'>
              {t('Collect usage')}
            </Label>
            <span
              className={cn(
                'rounded-full px-2 py-px text-[11px] font-medium whitespace-nowrap',
                collect ? COLLECT_ON.pill : COLLECT_OFF.pill
              )}
            >
              {collect ? t('Collecting') : t('Not collecting')}
            </span>
            <span className='flex-1' />
            <Switch
              id='cpa-collect'
              checked={collect}
              onCheckedChange={setCollect}
              className={COLLECT_SWITCH}
            />
          </div>
          <p
            className={cn(
              'mt-1.5 text-xs leading-relaxed',
              collect ? COLLECT_ON.text : COLLECT_OFF.text
            )}
          >
            {collect
              ? t('Recent requests are near real time, with per-account tokens and spend.')
              : t(
                  'If off: no token or spend data on cards, and "Recent request" can lag up to 10 minutes.'
                )}
          </p>
          <p className='text-muted-foreground mt-1.5 text-[11px] leading-normal'>
            {t(
              'Takes records from the CPA usage queue every 15 seconds · needs "Usage statistics" on in CPA · keep off if anything else reads this queue'
            )}
          </p>
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='cpa-channels'>{t('Channel IDs (optional)')}</Label>
          <Input
            id='cpa-channels'
            value={channelIds}
            onChange={(event) => setChannelIds(event.target.value)}
            placeholder='12,15'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'New API channels that point at this CPA, used to estimate revenue. Leave empty to match channels by host.'
            )}
          </p>
        </div>
      </div>
    </Dialog>
  )
}
