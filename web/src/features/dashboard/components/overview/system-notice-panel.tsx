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
import { Bell } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { RichContent } from '@/components/rich-content'
import { IconBadge } from '@/components/ui/icon-badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { getNotice } from '@/lib/api'

import { PanelWrapper } from '../ui/panel-wrapper'

interface SystemNoticePanelViewProps {
  notice: string
  loading: boolean
}

export function SystemNoticePanelView(props: SystemNoticePanelViewProps) {
  const { t } = useTranslation()

  return (
    <PanelWrapper
      title={
        <span className='flex items-center gap-2'>
          <IconBadge tone='info' size='sm'>
            <Bell />
          </IconBadge>
          {t('System Notice')}
        </span>
      }
      description={t('Latest platform updates and notices')}
      loading={props.loading}
      empty={!props.notice}
      emptyMessage={t('No announcements at this time')}
      height='h-72'
      contentClassName='p-0'
    >
      <ScrollArea className='h-72'>
        <div className='p-4 sm:p-5'>
          <RichContent
            breaks
            content={props.notice}
            className='text-sm leading-relaxed'
          />
        </div>
      </ScrollArea>
    </PanelWrapper>
  )
}

export function SystemNoticePanel() {
  const noticeQuery = useQuery({
    queryKey: ['notice'],
    queryFn: getNotice,
    staleTime: 5 * 60 * 1000,
  })
  const notice = noticeQuery.data?.success
    ? (noticeQuery.data.data ?? '').trim()
    : ''

  return (
    <SystemNoticePanelView notice={notice} loading={noticeQuery.isLoading} />
  )
}
