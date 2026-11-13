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
import { ServerCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { OVERVIEW_QUERY_KEY, getOverview } from './api'
import { MonitorPanel } from './components/monitor-panel'

/**
 * "Monitoring & alerts" page: pool and per-account RPM / TPM plus the alert
 * channels. Its own sidebar entry below "Subscription Accounts" (user
 * request 2026-11-12); it reads the same cached overview as that page.
 */
export function SubscriptionMonitor() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: OVERVIEW_QUERY_KEY,
    queryFn: getOverview,
    refetchInterval: 5000,
    refetchOnWindowFocus: true,
  })
  const data = query.data

  let content: React.ReactNode
  if (query.isLoading) {
    content = <LoadingState />
  } else if (query.isError || !data) {
    content = (
      <ErrorState
        description={getServerErrorMessage(query.error)}
        onRetry={() => query.refetch()}
      />
    )
  } else if (!data.instances.length) {
    content = (
      <EmptyState
        icon={ServerCog}
        title={t('No CPA added yet')}
        description={t(
          'Add a CPA remote management address to see its subscription accounts.'
        )}
      />
    )
  } else {
    content = <MonitorPanel data={data} />
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>{t('Monitoring & alerts')}</span>
          <Badge variant='outline' className='shrink-0'>
            Root
          </Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
