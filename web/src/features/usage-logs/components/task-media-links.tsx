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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import { taskMediaAddress } from '../lib/media-address'
import type { TaskMedia } from '../types'

function TaskMediaAddress(props: {
  label: string
  address: string
  openable?: boolean
}) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  return (
    <div className='grid min-w-0 gap-1'>
      <dt className='text-muted-foreground text-xs'>{props.label}</dt>
      <dd className='flex min-w-0 flex-wrap items-start gap-2'>
        <code className='min-w-0 flex-1 self-center text-xs break-all'>
          {props.address}
        </code>
        <div className='flex shrink-0 items-center gap-1'>
          <Button
            size='sm'
            variant='outline'
            aria-label={`${t('Copy')} ${props.label}`}
            onClick={() => void copyToClipboard(props.address)}
          >
            {t('Copy')}
          </Button>
          {props.openable && (
            <a
              href={props.address}
              target='_blank'
              rel='noopener noreferrer'
              referrerPolicy='no-referrer'
              aria-label={`${t('Open')} ${props.label}`}
              className='rounded-md px-2 py-1 text-xs underline underline-offset-4 focus-visible:ring-2'
            >
              {t('Open')}
            </a>
          )}
        </div>
      </dd>
    </div>
  )
}

// 保存真实地址而不是 blob 临时地址；文件到期后，即使弹窗仍开着也停止展示访问入口。
export function TaskMediaLinks(props: {
  media: TaskMedia
  expiresAt?: number
}) {
  const { t } = useTranslation()
  const [expired, setExpired] = useState(
    Boolean(props.expiresAt && props.expiresAt * 1000 <= Date.now())
  )
  useEffect(() => {
    const remaining = props.expiresAt
      ? props.expiresAt * 1000 - Date.now()
      : undefined
    setExpired(remaining !== undefined && remaining <= 0)
    if (remaining === undefined || remaining <= 0) return
    const timer = setTimeout(() => setExpired(true), remaining)
    return () => clearTimeout(timer)
  }, [props.expiresAt])
  if (expired) return null
  const origin = window.location.origin
  const preview = taskMediaAddress(props.media.preview_url, origin, 'preview')
  const content = taskMediaAddress(props.media.url, origin, 'api')
  const source = taskMediaAddress(props.media.source_url, origin, 'source')
  if (!preview && !content && !source) return null
  return (
    <div className='mt-3 grid gap-2 border-t pt-3'>
      <dl className='grid gap-3'>
        {preview && (
          <TaskMediaAddress
            label={t('Local preview URL')}
            address={preview}
            openable
          />
        )}
        {content && (
          <TaskMediaAddress label={t('Media API URL')} address={content} />
        )}
        {source && (
          <TaskMediaAddress
            label={t('Upstream media URL')}
            address={source}
            openable
          />
        )}
      </dl>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Open the local preview link in your browser after signing in. The media API requires authentication.'
        )}
      </p>
      {!props.media.role && !source && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'No upstream URL recorded. The provider may have returned image data directly; use the local preview link.'
          )}
        </p>
      )}
      {source && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Upstream links may expire earlier and are controlled by the provider.'
          )}
        </p>
      )}
    </div>
  )
}
