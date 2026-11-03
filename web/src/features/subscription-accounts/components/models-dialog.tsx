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
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { listAccountModels, type AccountRef } from '../api'

type ModelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  accountRef: AccountRef
}

export function ModelsDialog(props: ModelsDialogProps) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: [
      'subscription-accounts',
      'models',
      props.accountRef.instance_id,
      props.accountRef.name,
    ],
    queryFn: () => listAccountModels(props.accountRef),
    enabled: props.open,
  })

  let body: React.ReactNode
  if (query.isLoading) {
    body = <Loader2 className='mx-auto size-5 animate-spin' />
  } else if (query.isError) {
    body = (
      <div className='text-sm text-red-600'>
        {getServerErrorMessage(query.error)}
      </div>
    )
  } else if (!query.data?.length) {
    body = <div className='text-muted-foreground text-sm'>{t('No models')}</div>
  } else {
    body = (
      <div className='flex flex-wrap gap-1.5'>
        {query.data.map((model) => (
          <Badge key={model.id} variant='outline' title={model.display_name}>
            {model.id}
          </Badge>
        ))}
      </div>
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Models')}
      description={props.accountRef.name}
    >
      {body}
    </Dialog>
  )
}
