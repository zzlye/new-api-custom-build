import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  VideoProtocolForm,
  type VideoProtocol,
  type VideoProtocolCatalog,
} from './video-protocol-form'

export function VideoProtocolEditor(props: { channelId: number }) {
  const isRoot = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  // 普通管理员不挂载编辑器，也不发送读取协议的请求。
  return isRoot ? (
    <RootVideoProtocolEditor
      key={props.channelId}
      channelId={props.channelId}
    />
  ) : null
}

function RootVideoProtocolEditor(props: { channelId: number }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<VideoProtocol | null>(null)
  const catalog = useQuery({
    queryKey: ['video-protocol-presets'],
    enabled: open,
    queryFn: async () => {
      const res = await api.get('/api/video-protocol/')
      if (!res.data.success) throw new Error(res.data.message)
      return res.data.data as VideoProtocolCatalog
    },
  })
  const profile = useQuery({
    queryKey: ['video-protocol', props.channelId],
    enabled: open,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const res = await api.get(`/api/video-protocol/${props.channelId}`)
      if (!res.data.success) throw new Error(res.data.message)
      return res.data.data as VideoProtocol
    },
  })
  useEffect(() => {
    if (profile.data) setDraft(profile.data)
  }, [profile.data])
  const save = useMutation({
    mutationFn: async () => {
      const res = await api.put(`/api/video-protocol/${props.channelId}`, draft)
      if (!res.data.success) throw new Error(res.data.message)
      return res.data.data as VideoProtocol
    },
    onSuccess: (data) => setDraft(data),
  })
  return (
    <details
      className='min-w-0 rounded-xl border p-4'
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary className='cursor-pointer font-semibold'>
        {t('Video channel protocol')}
      </summary>
      <div className='mt-4 space-y-4'>
        <p className='text-sm opacity-70'>
          {t(
            'Configure this channel once for all video models. Existing tasks keep their saved protocol.'
          )}
        </p>
        {catalog.isError || profile.isError ? (
          <p role='alert'>{(catalog.error || profile.error)?.message}</p>
        ) : null}
        {profile.isPending && open ? (
          <p role='status'>{t('Loading...')}</p>
        ) : null}
        {draft && catalog.data ? (
          <>
            <label className='block text-sm'>
              {t('Protocol preset')}
              <select
                className='ml-2 rounded-lg border bg-transparent p-2'
                value=''
                disabled={save.isPending}
                onChange={(e) => {
                  const next = catalog.data.presets[e.target.value]
                  if (next) {
                    setDraft(structuredClone(next))
                    save.reset()
                  }
                }}
              >
                <option value=''>{t('Choose preset')}</option>
                {Object.keys(catalog.data.presets).map((key) => (
                  <option key={key} value={key}>
                    {t(`video.preset.${key}`)}
                  </option>
                ))}
              </select>
            </label>
            <VideoProtocolForm
              value={draft}
              onChange={(value) => {
                setDraft(value)
                save.reset()
              }}
              t={(key) => t(key)}
              disabled={save.isPending}
            />
            <button
              type='button'
              className='rounded-lg border px-4 py-2 text-sm font-medium'
              disabled={save.isPending}
              onClick={(e) => {
                const section = e.currentTarget.closest('details')
                const invalid =
                  section?.querySelector<HTMLTextAreaElement>(
                    'textarea:invalid'
                  )
                if (invalid) {
                  invalid.reportValidity()
                  return
                }
                save.mutate()
              }}
            >
              {t('Save video protocol')}
            </button>
            {save.error ? <p role='alert'>{save.error.message}</p> : null}
            {save.isSuccess ? (
              <p role='status'>{t('Video protocol saved')}</p>
            ) : null}
          </>
        ) : null}
      </div>
    </details>
  )
}
