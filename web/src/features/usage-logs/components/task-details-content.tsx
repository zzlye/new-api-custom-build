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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'

import type { TaskDetails, TaskMedia } from '../types'
import { TaskMediaPreview } from './task-media-preview'

const PARAMETER_LABELS: Record<string, string> = {
  model: 'Model',
  size: 'Size',
  quality: 'Quality',
  n: 'Quantity',
  output_format: 'Output format',
  response_format: 'Response format',
  background: 'Background',
  moderation: 'Moderation',
  seconds: 'Duration',
  duration: 'Duration',
  aspect_ratio: 'Aspect ratio',
  resolution: 'Resolution',
  seed: 'Seed',
  negative_prompt: 'Negative prompt',
  stream: 'Streaming',
}
const STATUS_LABELS: Record<string, string> = {
  pending: 'Queued',
  processing: 'Generating',
  waiting: 'Running',
  succeeded: 'Success',
  failed: 'Failed',
  cancelled: 'Cancelled',
}

function TaskMediaGallery(props: {
  media: TaskMedia[]
  expiresAt?: number
  references?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div
      className={
        props.references
          ? 'grid gap-4 sm:grid-cols-2 lg:grid-cols-3'
          : 'grid gap-4'
      }
    >
      {props.media.map((media, index) => (
        <figure
          key={media.url || `${media.role}-${index}`}
          className='bg-muted/20 min-w-0 rounded-lg border p-3'
        >
          {props.references && (
            <figcaption
              className='text-muted-foreground mb-2 truncate text-xs'
              title={media.name}
            >
              {media.role === 'mask'
                ? t('Mask image')
                : t('Reference image {{number}}', { number: index + 1 })}
              {media.name && ` · ${media.name}`}
            </figcaption>
          )}
          <TaskMediaPreview
            media={media}
            index={index}
            expiresAt={props.expiresAt}
          />
        </figure>
      ))}
    </div>
  )
}

// 详情只在展开时加载，运行中的记录自动刷新；页面关闭后停止查询，不影响后台生成。
export function TaskDetailsContent(props: { taskId: string }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['async-task-details', props.taskId],
    queryFn: async ({ signal }) => {
      const response = await api.get<{
        success: boolean
        data: TaskDetails
        message?: string
      }>(`/api/task/${encodeURIComponent(props.taskId)}/details`, {
        signal,
        disableDuplicate: true,
        skipErrorHandler: true,
      })
      if (!response.data.success) {
        throw new Error(response.data.message || 'Failed to load task details')
      }
      return response.data.data
    },
    refetchInterval: (state) => {
      const status = state.state.data?.status
      return status && ['pending', 'processing', 'waiting'].includes(status)
        ? 3000
        : false
    },
    staleTime: 0,
    retry: false,
  })
  if (query.isPending) {
    return (
      <p role='status' className='text-muted-foreground py-6 text-sm'>
        {t('Loading task details...')}
      </p>
    )
  }
  if (!query.data || query.isError) {
    return (
      <div role='alert' className='grid justify-items-start gap-3 py-6'>
        <p className='text-sm text-red-600'>
          {t('Failed to load task details')}
        </p>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void query.refetch()}
        >
          {t('Retry')}
        </Button>
      </div>
    )
  }
  const details = query.data
  const timeItems = [
    { label: t('Submit Time'), time: details.submit_time },
    { label: t('Generation started at'), time: details.start_time },
    { label: t('API responded at'), time: details.response_time },
    { label: t('Task finished at'), time: details.finish_time },
    { label: t('Media expires at'), time: details.expires_at },
  ]
  const duration =
    details.response_time && details.start_time
      ? Math.max(0, details.response_time - details.start_time)
      : null
  return (
    <div className='grid min-w-0 gap-6'>
      <dl className='grid gap-3 text-sm sm:grid-cols-2'>
        <div className='min-w-0 sm:col-span-2'>
          <dt className='text-muted-foreground text-xs'>{t('Task ID')}</dt>
          <dd className='mt-1 font-mono text-xs break-all'>
            {details.task_id}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-xs'>{t('API endpoint')}</dt>
          <dd className='mt-1 font-mono text-xs break-all'>
            {details.request_method} {details.request_path}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>{t('Model')}</dt>
          <dd className='mt-1 break-all'>{details.model_name || '-'}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>{t('Status')}</dt>
          <dd className='mt-1'>
            {t(STATUS_LABELS[details.status] || details.status)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Response status')}
          </dt>
          <dd className='mt-1 font-mono'>
            {details.response_status_code || '-'}
          </dd>
        </div>
      </dl>
      <dl className='bg-muted/30 grid gap-3 rounded-lg border p-4 sm:grid-cols-2 lg:grid-cols-3'>
        {timeItems.map((item) => (
          <div key={item.label}>
            <dt className='text-muted-foreground text-xs'>{item.label}</dt>
            <dd className='mt-1 font-mono text-xs'>
              {item.time ? formatTimestampToDate(item.time, 'seconds') : '-'}
            </dd>
          </div>
        ))}
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Request duration')}
          </dt>
          <dd className='mt-1 font-mono text-xs'>
            {duration === null ? '-' : `${duration}s`}
          </dd>
        </div>
      </dl>
      {details.error && (
        <p
          role='alert'
          className='rounded-lg border border-red-200 bg-red-50 p-3 text-sm break-words whitespace-pre-wrap text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300'
        >
          {details.error}
        </p>
      )}
      <section className='grid gap-2' aria-label={t('Prompt')}>
        <h3 className='text-sm font-semibold'>{t('Prompt')}</h3>
        {!details.input_available && (
          <p className='text-muted-foreground text-xs'>
            {t('Original input was not saved for this historical task.')}
          </p>
        )}
        {details.prompt_source === 'upstream_revised' && (
          <p className='text-muted-foreground text-xs'>
            {t(
              'The text below is the revised prompt returned by the provider.'
            )}
          </p>
        )}
        {details.input_error && (
          <p className='text-muted-foreground text-xs'>{details.input_error}</p>
        )}
        <pre className='bg-muted/20 max-h-64 overflow-auto rounded-lg border p-4 font-sans text-sm break-words whitespace-pre-wrap'>
          {details.prompt || t('No prompt recorded')}
        </pre>
      </section>
      {details.parameters && Object.keys(details.parameters).length > 0 && (
        <section className='grid gap-2' aria-label={t('Generation parameters')}>
          <h3 className='text-sm font-semibold'>
            {t('Generation parameters')}
          </h3>
          <dl className='grid gap-3 rounded-lg border p-4 sm:grid-cols-2 lg:grid-cols-3'>
            {Object.entries(details.parameters).map(([name, value]) => (
              <div key={name} className='min-w-0'>
                <dt className='text-muted-foreground text-xs'>
                  {t(PARAMETER_LABELS[name] || name)}
                </dt>
                <dd className='mt-1 text-sm break-words whitespace-pre-wrap'>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        </section>
      )}
      <section className='grid gap-3' aria-label={t('Reference images')}>
        <h3 className='text-sm font-semibold'>{t('Reference images')}</h3>
        {details.media_expired && details.references.length > 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('Reference media have expired')}
          </p>
        ) : (
          <TaskMediaGallery
            media={details.references}
            references
            expiresAt={details.expires_at}
          />
        )}
        {details.references.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t(
              details.input_available
                ? 'No reference images'
                : 'Reference images were not recorded for this historical task.'
            )}
          </p>
        )}
      </section>
      <section className='grid gap-3' aria-label={t('Generated media')}>
        <h3 className='text-sm font-semibold'>{t('Generated media')}</h3>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Reference and generated media share the retention period. Text details remain in the task log.'
          )}
        </p>
        {details.media_expired ? (
          <p className='text-muted-foreground text-sm'>
            {t('Generated files have expired')}
          </p>
        ) : (
          <TaskMediaGallery
            media={details.media || []}
            expiresAt={details.expires_at}
          />
        )}
        {!details.media_expired && !details.media?.length && (
          <p className='text-muted-foreground text-sm'>
            {t('No generated media available')}
          </p>
        )}
      </section>
    </div>
  )
}
