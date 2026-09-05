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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { TaskDetailsContent } from './task-details-content'

// 弹窗独立于表格行，刷新、排序、筛选和分页只更新列表，不改变当前选中的任务。
export function TaskDetailsDialog(props: {
  taskId: string | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog
      open={props.taskId !== null}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-5xl'>
        <DialogHeader>
          <DialogTitle>{t('Task details')}</DialogTitle>
          <DialogDescription>
            {t(
              'Request, prompt, references, generated media and timing are recorded together.'
            )}
          </DialogDescription>
        </DialogHeader>
        {props.taskId && (
          <TaskDetailsContent key={props.taskId} taskId={props.taskId} />
        )}
      </DialogContent>
    </Dialog>
  )
}
