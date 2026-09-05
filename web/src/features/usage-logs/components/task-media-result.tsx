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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { api } from '@/lib/api'

import type { TaskLog, TaskMedia } from '../types'

type TaskMediaPreviewProps = {
  media: TaskMedia
  expiresAt?: number
  index: number
}

// 通过已登录的请求读取媒体，再使用短生命周期地址展示，认证信息不会出现在图片地址里。
export function TaskMediaPreview(props: TaskMediaPreviewProps) {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    // 保存期限或媒体地址变化时清空旧状态，让仍可读取的结果重新展示。
    setUrl('')
    setError('')
    const controller = new AbortController()
    let objectUrl = ''
    let active = true
    let timer: ReturnType<typeof setTimeout> | undefined
    const expire = () => {
      controller.abort()
      if (objectUrl) URL.revokeObjectURL(objectUrl)
      setUrl('')
      setError('Generated files have expired')
    }
    const remaining = props.expiresAt
      ? props.expiresAt * 1000 - Date.now()
      : undefined
    if (remaining !== undefined && remaining <= 0) {
      setError('Generated files have expired')
      return () => controller.abort()
    }
    if (remaining !== undefined) timer = setTimeout(expire, remaining)
    api
      .get<Blob>(props.media.url, {
        responseType: 'blob',
        signal: controller.signal,
        disableDuplicate: true,
        skipErrorHandler: true,
      })
      .then((response) => {
        if (!active || controller.signal.aborted) return
        objectUrl = URL.createObjectURL(response.data)
        setUrl(objectUrl)
      })
      .catch(() => {
        if (active && !controller.signal.aborted) {
          setError('Failed to load generated media')
        }
      })
    return () => {
      active = false
      controller.abort()
      if (timer) clearTimeout(timer)
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [props.media.url, props.expiresAt])
  if (error) {
    return (
      <p role='alert' className='text-muted-foreground text-sm'>
        {t(error)}
      </p>
    )
  }
  if (!url) {
    return (
      <p role='status' className='text-muted-foreground text-sm'>
        {t('Loading generated media...')}
      </p>
    )
  }
  if (props.media.kind === 'video') {
    return (
      <video
        src={url}
        controls
        preload='metadata'
        aria-label={t('Generated video')}
        className='max-h-[65vh] w-full rounded-md'
      />
    )
  }
  return (
    <img
      src={url}
      alt={t('Generated image {{number}}', { number: props.index + 1 })}
      className='max-h-[65vh] w-full rounded-md object-contain'
    />
  )
}

// 预览窗口打开后才下载生成文件，避免任务列表同时载入大量图片或视频。
export function TaskMediaResult(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  if (props.log.media_expired) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Generated files have expired')}
      </span>
    )
  }
  if (!props.log.media?.length) return null
  return (
    <>
      <Button variant='ghost' size='sm' onClick={() => setOpen(true)}>
        {t('View generated media')}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t('Generated media')}</DialogTitle>
            <DialogDescription>
              {t(
                'Generated files are automatically removed after the retention period. Task logs are kept.'
              )}
            </DialogDescription>
          </DialogHeader>
          {open && (
            <div className='grid gap-4'>
              {props.log.media.map((media, index) => (
                <TaskMediaPreview
                  key={media.url}
                  media={media}
                  index={index}
                  expiresAt={props.log.expires_at}
                />
              ))}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
