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
import { useTranslation } from 'react-i18next'

import { useCacheStatsVisible } from '@/hooks/use-cache-stats-visible'
import { formatPercent } from '@/lib/format'

import type { UsageLog } from '../data/schema'

export function CacheHitRate(props: { log: UsageLog }) {
  const { t } = useTranslation()
  const visible = useCacheStatsVisible()
  // 仅呈现后端已授权且完成口径归一化的数据，不从原始缓存数字自行推算。
  if (!visible || !props.log.cache_usage) return null
  return (
    <span className='text-muted-foreground text-xs tabular-nums'>
      {t('Cache hit rate')} {formatPercent(props.log.cache_usage.hit_rate)}
    </span>
  )
}
