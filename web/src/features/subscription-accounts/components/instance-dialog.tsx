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
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import {
  OVERVIEW_QUERY_KEY,
  createInstance,
  testInstance,
  updateInstance,
} from '../api'
import type { CpaInstance } from '../types'

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
        <div className='space-y-1.5 rounded-md border p-3'>
          <div className='flex items-center justify-between gap-3'>
            <Label htmlFor='cpa-collect'>{t('Collect usage')}</Label>
            <Switch
              id='cpa-collect'
              checked={collect}
              onCheckedChange={setCollect}
            />
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Reads and removes records from the CPA usage queue every 15 seconds to show per-account tokens and spend. Requires usage-statistics-enabled in the CPA config. Leave off if anything else reads that queue.'
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
