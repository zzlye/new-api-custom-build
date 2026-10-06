import { useMutation } from '@tanstack/react-query'
import { useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import type { AdapterRegistry } from './index'

const maxFileBytes = 2 * 1024 * 1024

interface Props {
  registry: AdapterRegistry
  channels: { id: number; name: string }[]
  disabled?: boolean
  onImport: (registry: AdapterRegistry) => void
  onOpenChange: (open: boolean) => void
}

// 下载和导出共用文件处理，释放临时地址但不影响正在启动的下载。
function downloadAdapterFile(content: string, filename: string): void {
  const blob = new Blob([content], {
    type: filename.endsWith('.json')
      ? 'application/json;charset=utf-8'
      : 'text/markdown;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.append(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

export function AdapterConfigTransfer(props: Props) {
  const { t } = useTranslation()
  const id = useId()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [channelIds, setChannelIds] = useState<number[]>([])
  const [channelSearch, setChannelSearch] = useState('')
  const [fileError, setFileError] = useState('')
  const [reading, setReading] = useState(false)
  const fileRequest = useRef(0)
  // 搜索只影响可见渠道，不改变已选绑定或已校验的导入预览。
  const channelQuery = channelSearch.trim().toLowerCase()
  const filteredChannels = props.channels.filter(
    (channel) =>
      channel.name.toLowerCase().includes(channelQuery) ||
      String(channel.id).includes(channelQuery)
  )
  const signature = JSON.stringify([props.registry, text, channelIds])
  const preview = useMutation({
    meta: { errorToast: false },
    mutationFn: async (request: {
      registry: AdapterRegistry
      text: string
      channelIds: number[]
      signature: string
    }) => {
      if (new Blob([request.text]).size > maxFileBytes) {
        throw new Error(t('The configuration file must not exceed 2 MiB.'))
      }
      let bundle: unknown
      try {
        bundle = JSON.parse(request.text.replace(/^\uFEFF/, ''))
      } catch {
        throw new Error(t('Enter valid JSON without comments or code fences.'))
      }
      const response = await api.post('/api/video-adapters/import-preview', {
        registry: request.registry,
        bundle,
        channel_ids: request.channelIds,
      })
      const data = requireServerSuccess(response.data).data as AdapterRegistry
      return { registry: data, signature: request.signature }
    },
  })
  const download = useMutation({
    mutationFn: async (kind: 'template' | 'guide') => {
      const response = await api.get<string>(
        `/api/video-adapters/import-${kind}`,
        { responseType: 'text' }
      )
      downloadAdapterFile(
        response.data,
        `video-adapters-${kind}.${kind === 'template' ? 'json' : 'md'}`
      )
    },
    onError: (error) => handleServerError(error),
  })
  // 文本、渠道或外层草稿有变化时，旧预览不可再用于覆盖新输入。
  const validPreview =
    preview.data?.signature === signature ? preview.data.registry : null
  const busy = reading || preview.isPending
  const changeOpen = (value: boolean) => {
    if (busy) return
    fileRequest.current++
    setOpen(value)
    setChannelSearch('')
    props.onOpenChange(value)
    preview.reset()
    setFileError('')
  }
  const addedTemplates =
    validPreview?.templates.slice(props.registry.templates.length) ?? []
  const addedRules =
    validPreview?.rules.slice(props.registry.rules.length) ?? []

  return (
    <>
      <div className='flex min-w-0 flex-wrap gap-2'>
        <Button
          type='button'
          variant='outline'
          disabled={props.disabled}
          onClick={() => changeOpen(true)}
        >
          {t('Import configuration')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={download.isPending}
          onClick={() => download.mutate('template')}
        >
          {t('Download configuration template')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={download.isPending}
          onClick={() => download.mutate('guide')}
        >
          {t('Download filling guide')}
        </Button>
        <Button
          type='button'
          variant='outline'
          onClick={() =>
            downloadAdapterFile(
              JSON.stringify(
                {
                  format: 'newapi-video-adapters',
                  format_version: 1,
                  templates: props.registry.templates,
                  rules: props.registry.rules,
                },
                null,
                2
              ),
              'video-adapters-config.json'
            )
          }
        >
          {t('Export current configuration')}
        </Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={changeOpen}
        title={t('Import video adapter configuration')}
        description={t(
          'Import adds templates and rules to the draft. Existing rules stay unchanged until you publish.'
        )}
        bodyClassName='space-y-4'
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              disabled={busy}
              onClick={() => changeOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={busy || !text.trim()}
              onClick={() => {
                setFileError('')
                preview.mutate({
                  registry: props.registry,
                  text,
                  channelIds,
                  signature,
                })
              }}
            >
              {preview.isPending
                ? t('Validating...')
                : t('Validate and preview')}
            </Button>
            <Button
              type='button'
              disabled={busy || !validPreview || props.disabled}
              onClick={() => {
                if (!validPreview) return
                props.onImport(validPreview)
                setText('')
                setChannelIds([])
                changeOpen(false)
              }}
            >
              {t('Add to draft')}
            </Button>
          </>
        }
      >
        <div className='space-y-2'>
          <Label htmlFor={`${id}-file`}>{t('Configuration JSON file')}</Label>
          <Input
            id={`${id}-file`}
            type='file'
            accept='.json,application/json'
            disabled={busy}
            onChange={async (event) => {
              const file = event.currentTarget.files?.[0]
              event.currentTarget.value = ''
              if (!file) return
              const request = ++fileRequest.current
              preview.reset()
              setFileError('')
              if (file.size > maxFileBytes) {
                setFileError(t('The configuration file must not exceed 2 MiB.'))
                return
              }
              setReading(true)
              try {
                const content = await file.text()
                if (request === fileRequest.current) {
                  setText(content.replace(/^\uFEFF/, ''))
                }
              } catch {
                if (request === fileRequest.current) {
                  setFileError(
                    t(
                      'Could not read the configuration file. Please select it again.'
                    )
                  )
                }
              } finally {
                if (request === fileRequest.current) setReading(false)
              }
            }}
          />
          {reading ? (
            <p role='status'>{t('Reading configuration file...')}</p>
          ) : null}
        </div>
        <div className='space-y-2'>
          <Label htmlFor={`${id}-json`}>
            {t('Or paste configuration JSON')}
          </Label>
          <Textarea
            id={`${id}-json`}
            rows={8}
            className='min-h-44 font-mono text-xs'
            value={text}
            disabled={busy}
            onChange={(event) => {
              setText(event.target.value)
              setFileError('')
              preview.reset()
            }}
          />
        </div>
        <fieldset disabled={busy} className='min-w-0 space-y-2'>
          <legend className='text-sm font-medium'>
            {t('Bind imported rules to channels')}
          </legend>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Selected channels replace every imported rule’s channel IDs. Leave unselected to keep the file’s bindings.'
            )}
          </p>
          <Input
            type='search'
            aria-label={t('Search channels by name or ID')}
            placeholder={t('Search channels by name or ID')}
            value={channelSearch}
            disabled={busy}
            onChange={(event) => setChannelSearch(event.target.value)}
          />
          <ScrollArea
            role='region'
            aria-label={t('Bind imported rules to channels')}
            className='h-48 w-full min-w-0 rounded-lg border'
          >
            <ul className='flex min-w-0 flex-col divide-y px-3'>
              {filteredChannels.map((channel) => (
                <li
                  key={channel.id}
                  className='flex min-w-0 items-center gap-3'
                >
                  <Checkbox
                    id={`${id}-channel-${channel.id}`}
                    checked={channelIds.includes(channel.id)}
                    disabled={busy}
                    onCheckedChange={(checked) => {
                      setChannelIds((current) =>
                        checked
                          ? [...current, channel.id]
                          : current.filter((value) => value !== channel.id)
                      )
                      preview.reset()
                    }}
                  />
                  <Label
                    className='min-h-11 min-w-0 flex-1 cursor-pointer py-3 text-sm leading-relaxed break-all'
                    htmlFor={`${id}-channel-${channel.id}`}
                  >
                    {channel.name} · {channel.id}
                  </Label>
                </li>
              ))}
            </ul>
            {!props.channels.length ? (
              <p className='text-muted-foreground p-3 text-sm'>
                {t(
                  'Create a channel before binding model rules. Templates can be imported without rules.'
                )}
              </p>
            ) : null}
            {props.channels.length > 0 && !filteredChannels.length ? (
              <p className='text-muted-foreground p-3 text-sm'>
                {t('No channels found')}
              </p>
            ) : null}
          </ScrollArea>
          <p className='text-muted-foreground text-xs'>
            {t('Selected {{count}}', { count: channelIds.length })}
          </p>
        </fieldset>
        {fileError || preview.isError ? (
          <p role='alert' className='text-destructive text-sm break-words'>
            {fileError || getServerErrorMessage(preview.error)}
          </p>
        ) : null}
        {preview.data && !validPreview ? (
          <p role='status'>
            {t('The draft has changed. Validate the import again.')}
          </p>
        ) : null}
        {validPreview ? (
          <section
            aria-label={t('Import preview')}
            className='min-w-0 space-y-2 rounded-lg border p-3 text-sm'
          >
            <p role='status'>
              {t(
                'Validation passed: {{templates}} templates and {{rules}} rules will be added.',
                { templates: addedTemplates.length, rules: addedRules.length }
              )}
            </p>
            <ul className='list-inside list-disc space-y-1 break-all'>
              {addedRules.map((rule) => (
                <li key={rule.id}>
                  {rule.models.join(', ') || t('Channel default')} ·{' '}
                  {rule.enabled ? t('Enabled') : t('Disabled')} ·{' '}
                  {rule.channel_ids
                    .map(
                      (value) =>
                        props.channels.find((channel) => channel.id === value)
                          ?.name ?? value
                    )
                    .join(', ')}
                </li>
              ))}
            </ul>
            <details>
              <summary className='cursor-pointer'>
                {t('Review imported configuration')}
              </summary>
              <pre className='mt-2 max-h-64 overflow-auto text-xs break-all whitespace-pre-wrap'>
                {JSON.stringify(
                  { templates: addedTemplates, rules: addedRules },
                  null,
                  2
                )}
              </pre>
            </details>
          </section>
        ) : null}
      </Dialog>
    </>
  )
}
