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

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { policyLabel } from '@/features/system-settings/request-policies/policy-label'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { TaskRoutingEvent } from '../types'

// 直接展示任务保存的管理员路由摘要；打开详情不会触发重试或自动刷新。
export function TaskRoutingHistory(props: { events?: TaskRoutingEvent[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  if (!props.events?.length) return null
  return (
    <section
      className='min-w-0 space-y-2'
      aria-label={t('Media routing history')}
    >
      <h3 className='text-sm font-semibold'>{t('Media routing history')}</h3>
      <div className='max-h-80 overflow-auto rounded-lg border'>
        <Table withContainer={false}>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Attempt')}</TableHead>
              <TableHead>{t('Channel')}</TableHead>
              <TableHead>HTTP</TableHead>
              <TableHead>{t('Decision')}</TableHead>
              <TableHead>{t('Elapsed')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.events.map((event) => (
              <TableRow
                key={`${event.attempt}-${event.channel_id}-${event.decision.action}-${event.decision.reason}`}
              >
                <TableCell>{formatNumber(event.attempt, locale)}</TableCell>
                <TableCell>
                  {event.channel_id ? `#${event.channel_id}` : '-'}
                  {event.group && (
                    <div className='text-muted-foreground text-xs'>
                      {event.group}
                    </div>
                  )}
                </TableCell>
                <TableCell>{event.status || '-'}</TableCell>
                <TableCell className='min-w-44 whitespace-normal'>
                  {policyLabel(t, event.decision.reason)}
                  {event.decision.action === 'retry' && (
                    <span className='text-primary ml-2'>
                      {t(
                        event.decision.reason === 'retry_same_channel'
                          ? 'Retry current channel'
                          : 'Switch channel'
                      )}
                    </span>
                  )}
                  {event.error_code && (
                    <div className='text-muted-foreground text-xs break-all'>
                      {event.error_code}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  {formatNumber(event.elapsed_ms, locale)} ms
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}
