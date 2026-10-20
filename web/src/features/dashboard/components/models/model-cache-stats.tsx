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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { useCacheStatsVisible } from '@/hooks/use-cache-stats-visible'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatPercent } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { computeTimeRange } from '@/lib/time'
import { useAuthStore } from '@/stores/auth-store'

import { getModelCacheStats } from '../../api'
import { getDefaultDays } from '../../lib'
import type { DashboardFilters } from '../../types'

export function ModelCacheStatistics(props: { filters: DashboardFilters }) {
  const { t, i18n } = useTranslation()
  const visible = useCacheStatsVisible()
  const user = useAuthStore((state) => state.auth.user)
  const params = useMemo(
    () => ({
      ...computeTimeRange(
        getDefaultDays(props.filters.time_granularity),
        props.filters.start_timestamp,
        props.filters.end_timestamp
      ),
      username:
        user?.role === ROLE.SUPER_ADMIN ? props.filters.username : undefined,
    }),
    [props.filters, user?.role]
  )
  const query = useQuery({
    // 用户切换或权限改变不能复用另一个人的统计缓存。
    queryKey: ['model-cache-statistics', user?.id, user?.role, visible, params],
    queryFn: ({ signal }) => getModelCacheStats(params, signal),
    enabled: visible,
    retry: false,
    refetchOnWindowFocus: true,
  })
  if (!visible) return null
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  let content
  if (query.isPending) content = <LoadingState />
  else if (query.isError) {
    content = (
      <ErrorState
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  } else {
    content = (
      <StaticDataTable
        className='max-h-96 overflow-auto rounded-none border-0'
        data={query.data ?? []}
        getRowKey={(row) => row.model_name}
        emptyContent={t('No data')}
        columns={[
          { id: 'model', header: t('Model'), cell: (row) => row.model_name },
          {
            id: 'input',
            header: t('Input Tokens'),
            cell: (row) => formatNumber(row.input_tokens, locale),
          },
          {
            id: 'read',
            header: t('Cache Read'),
            cell: (row) => formatNumber(row.read_tokens, locale),
          },
          {
            id: 'write',
            header: t('Cache Write'),
            cell: (row) => formatNumber(row.write_tokens, locale),
          },
          {
            id: 'hit',
            header: t('Cache hit rate'),
            cell: (row) => formatPercent(row.hit_rate),
          },
        ]}
      />
    )
  }

  return (
    <section
      className='appearance-glass-surface overflow-hidden rounded-lg border'
      aria-label={t('Model cache statistics')}
    >
      <h2 className='border-b px-4 py-3 text-sm font-semibold'>
        {t('Model cache statistics')}
      </h2>
      {content}
    </section>
  )
}
