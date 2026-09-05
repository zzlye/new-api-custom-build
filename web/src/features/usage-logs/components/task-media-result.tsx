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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import type { TaskLog } from '../types'
import { TaskDetailsContent } from './task-details-content'

export { TaskMediaPreview } from './task-media-preview'

// 所有后台任务均可查看完整记录，失败或媒体过期不会隐藏提示词和请求信息。
export function TaskMediaResult(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <>
      <div className='flex min-w-0 flex-col items-start gap-1'>
        {props.log.fail_reason && (
          <button
            type='button'
            onClick={() => setOpen(true)}
            title={t('Click to view full error message')}
            className='max-w-[230px] truncate text-left text-xs text-red-600 hover:underline dark:text-red-400'
          >
            {props.log.fail_reason}
          </button>
        )}
        {!props.log.fail_reason && props.log.media_expired && (
          <span className='text-muted-foreground text-xs'>
            {t('Generated files have expired')}
          </span>
        )}
        <Button variant='ghost' size='sm' onClick={() => setOpen(true)}>
          {t('View task details')}
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-5xl'>
          <DialogHeader>
            <DialogTitle>{t('Task details')}</DialogTitle>
            <DialogDescription>
              {t(
                'Request, prompt, references, generated media and timing are recorded together.'
              )}
            </DialogDescription>
          </DialogHeader>
          {open && <TaskDetailsContent taskId={props.log.task_id} />}
        </DialogContent>
      </Dialog>
    </>
  )
}
