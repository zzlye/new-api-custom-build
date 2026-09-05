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

import { Button } from '@/components/ui/button'

import { useTaskDetails } from '../hooks/use-task-details'
import { TaskMediaLinks } from './task-media-links'
import { TaskMediaPreview } from './task-media-preview'

// 独立预览页沿用登录认证，链接本身不含密钥；打开时仍由服务器检查任务归属和保存期限。
export function TaskMediaView(props: {
  taskId: string
  kind: string
  index: string
}) {
  const { t } = useTranslation()
  const index = Number(props.index)
  const valid =
    /^async_[A-Za-z0-9_-]+$/.test(props.taskId) &&
    ['media', 'reference'].includes(props.kind) &&
    /^(0|[1-9][0-9]*)$/.test(props.index) &&
    index < 128
  const query = useTaskDetails(props.taskId, valid)
  if (!valid) return <p role='alert'>{t('Media preview is unavailable')}</p>
  if (query.isPending) {
    return <p role='status'>{t('Loading task details...')}</p>
  }
  if (!query.data || query.isError) {
    return (
      <div role='alert' className='grid justify-items-start gap-3'>
        <p>{t('Failed to load task details')}</p>
        <Button variant='outline' onClick={() => void query.refetch()}>
          {t('Retry')}
        </Button>
      </div>
    )
  }
  const details = query.data
  if (details.media_expired) {
    return <p role='alert'>{t('Generated files have expired')}</p>
  }
  const media = (
    props.kind === 'reference' ? details.references : details.media
  )?.[index]
  if (!media) return <p role='alert'>{t('Media preview is unavailable')}</p>
  return (
    <article className='mx-auto grid w-full max-w-5xl gap-4 rounded-lg border p-4'>
      <div>
        <h2 className='text-sm font-semibold'>{t('Task ID')}</h2>
        <p className='text-muted-foreground font-mono text-xs break-all'>
          {details.task_id}
        </p>
      </div>
      {media.name && <p className='text-sm break-all'>{media.name}</p>}
      <TaskMediaPreview
        media={media}
        expiresAt={details.expires_at}
        index={index}
      />
      <TaskMediaLinks media={media} expiresAt={details.expires_at} />
    </article>
  )
}
