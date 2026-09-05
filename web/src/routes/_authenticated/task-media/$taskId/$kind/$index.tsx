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
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { TaskMediaView } from '@/features/usage-logs/components/task-media-view'

// 使用现有登录路由，未登录时返回登录页，登录成功后继续打开原媒体地址。
export const Route = createFileRoute(
  '/_authenticated/task-media/$taskId/$kind/$index'
)({
  component: TaskMediaPage,
})

function TaskMediaPage() {
  const { t } = useTranslation()
  const params = Route.useParams()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Media preview')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <TaskMediaView
          taskId={params.taskId}
          kind={params.kind}
          index={params.index}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
