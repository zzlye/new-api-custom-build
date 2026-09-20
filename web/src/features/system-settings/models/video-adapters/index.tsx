import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { nanoid } from 'nanoid'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  VideoProtocolForm,
  type VideoProtocol,
} from '@/features/channels/components/video-protocol-form'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CapabilityEditor } from './capability-editor'

export interface AdapterTemplate {
  id: string
  name: string
  protocol: VideoProtocol
}
export interface AdapterRule {
  id: string
  enabled: boolean
  channel_ids: number[]
  models: string[]
  template_id: string
  override?: VideoProtocol
}
export interface AdapterRegistry {
  version: number
  templates: AdapterTemplate[]
  rules: AdapterRule[]
}
interface Channel {
  id: number
  name: string
  models: string
  model_mapping?: string
}
interface Catalog {
  registry: AdapterRegistry
  channels: Channel[]
}

const fieldClass =
  'w-full min-w-0 rounded-lg border bg-background px-3 py-2 text-sm'
const buttonClass =
  'rounded-lg border px-3 py-2 text-sm hover:bg-muted disabled:opacity-50'

export function VideoAdaptersSection() {
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  return root ? <VideoAdaptersManager /> : null
}

function VideoAdaptersManager() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [draft, setDraft] = useState<AdapterRegistry | null>(null)
  const [search, setSearch] = useState('')
  const [documentChannels, setDocumentChannels] = useState<number[]>([])
  const [activeTemplate, setActiveTemplate] = useState('')
  const [message, setMessage] = useState('')
  const catalog = useQuery({
    queryKey: ['video-adapters'],
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const r = await api.get('/api/video-adapters')
      if (!r.data.success) throw new Error(r.data.message)
      return r.data.data as Catalog
    },
  })
  useEffect(() => {
    if (catalog.data) setDraft(structuredClone(catalog.data.registry))
  }, [catalog.data])
  const save = useMutation({
    mutationFn: async () => {
      const r = await api.put('/api/video-adapters', draft)
      if (!r.data.success) throw new Error(r.data.message)
      return r.data.data as AdapterRegistry
    },
    onSuccess: (registry) => {
      client.setQueryData<Catalog>(['video-adapters'], (old) =>
        old ? { ...old, registry } : old
      )
      setMessage(t('Video adapter rules published'))
    },
  })
  const dirty =
    draft &&
    catalog.data &&
    JSON.stringify(draft) !== JSON.stringify(catalog.data.registry)
  useEffect(() => {
    if (!dirty) return
    const prevent = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', prevent)
    return () => window.removeEventListener('beforeunload', prevent)
  }, [dirty])
  if (!draft || !catalog.data) {
    return (
      <p role={catalog.isError ? 'alert' : 'status'}>
        {catalog.error?.message ?? t('Loading...')}
      </p>
    )
  }
  const channels = catalog.data.channels
  const updateRule = (id: string, patch: Partial<AdapterRule>) =>
    setDraft({
      ...draft,
      rules: draft.rules.map((r) => (r.id === id ? { ...r, ...patch } : r)),
    })
  const updateTemplate = (id: string, patch: Partial<AdapterTemplate>) =>
    setDraft({
      ...draft,
      templates: draft.templates.map((p) =>
        p.id === id ? { ...p, ...patch } : p
      ),
    })
  const addRule = () =>
    setDraft({
      ...draft,
      rules: [
        ...draft.rules,
        {
          id: nanoid(),
          enabled: true,
          channel_ids: [],
          models: [],
          template_id: draft.templates[0]?.id ?? '',
        },
      ],
    })
  const visible = draft.rules.filter((r) =>
    `${r.models.join(' ')} ${r.template_id} ${r.channel_ids.map((id) => channels.find((c) => c.id === id)?.name ?? id).join(' ')}`
      .toLowerCase()
      .includes(search.toLowerCase())
  )
  const template = draft.templates.find((p) => p.id === activeTemplate)
  return (
    <form
      className='min-w-0 space-y-5'
      onSubmit={(e) => {
        e.preventDefault()
        setMessage('')
        save.mutate()
      }}
    >
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h2 className='text-xl font-semibold'>{t('Video adapters')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Configure once, then the workshop uses these rules automatically.'
            )}
          </p>
        </div>
        <div className='flex items-center gap-3'>
          <span className='text-muted-foreground text-xs'>
            {t('Version')} {draft.version}
          </span>
          <button
            type='submit'
            className={`${buttonClass} bg-primary text-primary-foreground`}
            disabled={save.isPending || !dirty}
          >
            {save.isPending ? t('Saving...') : t('Publish rules')}
          </button>
        </div>
      </div>
      {save.isError ? (
        <p role='alert' className='text-destructive'>
          {save.error.message}
        </p>
      ) : null}
      {message ? <p role='status'>{message}</p> : null}
      <details className='rounded-xl border p-4'>
        <summary className='cursor-pointer font-medium'>
          {t('How to configure video adapters')}
        </summary>
        <ol className='mt-3 list-inside list-decimal space-y-2 text-sm'>
          <li>
            {t(
              'Add a row, select channels, and enter the exact upstream model names. Multiple models can share a template.'
            )}
          </li>
          <li>
            {t(
              'Choose a documented template. Use a row override only when that model differs.'
            )}
          </li>
          <li>
            {t(
              'For a new field, declare its type and workshop visibility, then map extra_parameters.KEY to the upstream field.'
            )}
          </li>
          <li>
            {t(
              'Preview a request and response, then publish. Previews never create paid video tasks.'
            )}
          </li>
        </ol>
        <p className='text-muted-foreground mt-3 text-sm'>
          {t(
            'wan-3.0 uses duration; sd2-5-720p uses seconds. Reference images and first frames have different meanings.'
          )}
        </p>
      </details>
      <div className='flex flex-wrap gap-2'>
        <input
          aria-label={t('Search video adapters')}
          className={`${fieldClass} sm:max-w-sm`}
          placeholder={t('Search models or channels')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button type='button' className={buttonClass} onClick={addRule}>
          {t('Add row')}
        </button>
      </div>
      <details className='rounded-xl border p-4'>
        <summary className='cursor-pointer font-medium'>
          {t('Bind documented models in bulk')}
        </summary>
        <div className='mt-3 flex flex-wrap items-end gap-3'>
          <label className='min-w-0 text-sm'>
            {t('Applicable channels')}
            <select
              aria-label={t('Channels for documented models')}
              multiple
              className={`${fieldClass} mt-1 h-24`}
              value={documentChannels.map(String)}
              onChange={(e) =>
                setDocumentChannels(
                  Array.from(e.target.selectedOptions, (o) => Number(o.value))
                )
              }
            >
              {channels.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </label>
          <button
            type='button'
            disabled={!documentChannels.length}
            className={buttonClass}
            onClick={() => {
              const names = [
                'wan-3.0',
                'sd2-5-720p',
                'seedance-2-pro',
                'seedance-2-fast',
                'seedance-2-mini',
                'seedance-2.5-pro',
                'wan-3',
                'gemini-omni-1.1',
              ]
              const rules = names
                .filter((name) => draft.templates.some((p) => p.id === name))
                .flatMap((name) => {
                  const ids = documentChannels.filter(
                    (id) =>
                      !draft.rules.some(
                        (r) =>
                          r.enabled &&
                          r.channel_ids.includes(id) &&
                          r.models.includes(name)
                      )
                  )
                  return ids.length
                    ? [
                        {
                          id: nanoid(),
                          enabled: true,
                          channel_ids: ids,
                          models: [name],
                          template_id: name,
                        },
                      ]
                    : []
                })
              setDraft({ ...draft, rules: [...draft.rules, ...rules] })
            }}
          >
            {t('Add documented model rules')}
          </button>
        </div>
      </details>
      <fieldset disabled={save.isPending} className='min-w-0 space-y-3'>
        {!visible.length ? (
          <p className='text-muted-foreground text-sm'>
            {t('No matching adapter rules. Add a row to bind a template.')}
          </p>
        ) : null}
        {visible.map((rule) => {
          const selected = draft.templates.find(
            (p) => p.id === rule.template_id
          )
          const protocol = rule.override ?? selected?.protocol
          const modelNames = [
            ...new Set(
              channels
                .filter((c) => rule.channel_ids.includes(c.id))
                .flatMap((c) => {
                  let mapped: string[] = []
                  try {
                    mapped = Object.values(
                      JSON.parse(c.model_mapping || '{}') as Record<
                        string,
                        string
                      >
                    )
                  } catch {}
                  return [...c.models.split(','), ...mapped]
                })
            ),
          ].filter(Boolean)
          return (
            <article key={rule.id} className='min-w-0 rounded-xl border p-4'>
              <div className='grid items-start gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto]'>
                <label className='min-w-0 text-sm'>
                  {t('Applicable channels')}
                  <select
                    aria-label={t('Applicable channels')}
                    multiple
                    className={`${fieldClass} mt-1 h-24`}
                    value={rule.channel_ids.map(String)}
                    onChange={(e) =>
                      updateRule(rule.id, {
                        channel_ids: Array.from(
                          e.target.selectedOptions,
                          (option) => Number(option.value)
                        ),
                      })
                    }
                  >
                    {channels.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name} · {c.id}
                      </option>
                    ))}
                  </select>
                </label>
                <div className='min-w-0 space-y-2'>
                  <label className='block text-sm'>
                    {t('Upstream model names')}
                    <BufferedList
                      value={rule.models}
                      onChange={(models) => updateRule(rule.id, { models })}
                      label={t('Upstream model names')}
                    />
                  </label>
                  <select
                    className={fieldClass}
                    aria-label={t('Add existing model')}
                    value=''
                    onChange={(e) => {
                      if (e.target.value) {
                        updateRule(rule.id, {
                          models: [
                            ...new Set([...rule.models, e.target.value]),
                          ],
                        })
                      }
                    }}
                  >
                    <option value=''>{t('Add existing model')}</option>
                    {modelNames.map((name) => (
                      <option key={name}>{name}</option>
                    ))}
                  </select>
                  <p className='text-muted-foreground text-xs'>
                    {t('Empty model names apply as the channel default.')}
                  </p>
                </div>
                <label className='min-w-0 text-sm'>
                  {t('Protocol template')}
                  <select
                    className={`${fieldClass} mt-1`}
                    value={rule.template_id}
                    onChange={(e) =>
                      updateRule(rule.id, {
                        template_id: e.target.value,
                        override: undefined,
                      })
                    }
                  >
                    {draft.templates.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </label>
                <div className='flex flex-wrap items-center gap-2 md:flex-col md:items-start'>
                  <label className='flex items-center gap-2 text-sm'>
                    <input
                      type='checkbox'
                      checked={rule.enabled}
                      onChange={(e) =>
                        updateRule(rule.id, { enabled: e.target.checked })
                      }
                    />
                    {t('Enabled')}
                  </label>
                  <button
                    type='button'
                    className={buttonClass}
                    onClick={() =>
                      setDraft({
                        ...draft,
                        rules: [
                          ...draft.rules,
                          {
                            ...structuredClone(rule),
                            id: nanoid(),
                            enabled: false,
                          },
                        ],
                      })
                    }
                  >
                    {t('Duplicate')}
                  </button>
                  <button
                    type='button'
                    className={`${buttonClass} text-destructive`}
                    onClick={() =>
                      setDraft({
                        ...draft,
                        rules: draft.rules.filter((r) => r.id !== rule.id),
                      })
                    }
                  >
                    {t('Delete')}
                  </button>
                </div>
              </div>
              {protocol ? (
                <details className='mt-4 min-w-0'>
                  <summary className='cursor-pointer text-sm font-medium'>
                    {t('Model overrides and preview')}
                  </summary>
                  <div className='mt-4 space-y-5'>
                    <label className='flex items-center gap-2 text-sm'>
                      <input
                        type='checkbox'
                        checked={Boolean(rule.override)}
                        onChange={(e) =>
                          updateRule(rule.id, {
                            override: e.target.checked
                              ? structuredClone(protocol)
                              : undefined,
                          })
                        }
                      />
                      {t('Customize this row')}
                    </label>
                    {rule.override ? (
                      <>
                        <CapabilityEditor
                          value={protocol.capabilities}
                          onChange={(capabilities) =>
                            updateRule(rule.id, {
                              override: { ...protocol, capabilities },
                            })
                          }
                        />
                        <VideoProtocolForm
                          showEnabled={false}
                          value={protocol}
                          onChange={(override) =>
                            updateRule(rule.id, { override })
                          }
                          t={t}
                        />
                      </>
                    ) : (
                      <p className='text-muted-foreground text-sm'>
                        {t('This row inherits the selected template.')}
                      </p>
                    )}
                    <AdapterPreview
                      protocol={protocol}
                      model={rule.models[0] ?? ''}
                      registry={draft}
                      channelIds={rule.channel_ids}
                    />
                  </div>
                </details>
              ) : null}
            </article>
          )
        })}
      </fieldset>
      <details className='min-w-0 rounded-xl border p-4'>
        <summary className='cursor-pointer font-medium'>
          {t('Manage reusable templates')}
        </summary>
        <div className='mt-4 space-y-4'>
          <div className='flex flex-wrap gap-2'>
            <select
              aria-label={t('Edit template')}
              className={`${fieldClass} sm:max-w-md`}
              value={activeTemplate}
              onChange={(e) => setActiveTemplate(e.target.value)}
            >
              <option value=''>{t('Choose template')}</option>
              {draft.templates.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <button
              type='button'
              className={buttonClass}
              onClick={() => {
                const source = template ?? draft.templates[0]
                if (!source) return
                const copy = {
                  ...structuredClone(source),
                  id: nanoid(),
                  name: `${source.name} ${t('Copy')}`,
                }
                setDraft({ ...draft, templates: [...draft.templates, copy] })
                setActiveTemplate(copy.id)
              }}
            >
              {t('New template from copy')}
            </button>
            {template ? (
              <button
                type='button'
                className={buttonClass}
                disabled={draft.rules.some(
                  (r) => r.template_id === template.id
                )}
                onClick={() => {
                  setDraft({
                    ...draft,
                    templates: draft.templates.filter(
                      (p) => p.id !== template.id
                    ),
                  })
                  setActiveTemplate('')
                }}
              >
                {t('Delete template')}
              </button>
            ) : null}
          </div>
          {template ? (
            <div key={template.id} className='space-y-5'>
              <label className='block text-sm'>
                {t('Template name')}
                <input
                  className={fieldClass}
                  value={template.name}
                  onChange={(e) =>
                    updateTemplate(template.id, { name: e.target.value })
                  }
                />
              </label>
              <CapabilityEditor
                value={template.protocol.capabilities}
                onChange={(capabilities) =>
                  updateTemplate(template.id, {
                    protocol: { ...template.protocol, capabilities },
                  })
                }
              />
              <VideoProtocolForm
                showEnabled={false}
                value={template.protocol}
                onChange={(protocol) =>
                  updateTemplate(template.id, { protocol })
                }
                t={t}
              />
              <AdapterPreview
                protocol={template.protocol}
                model={template.name}
              />
            </div>
          ) : null}
        </div>
      </details>
    </form>
  )
}

function BufferedList({
  value,
  onChange,
  label,
}: {
  value: string[]
  onChange: (next: string[]) => void
  label: string
}) {
  const serialized = value.join(',')
  const [text, setText] = useState(serialized)
  useEffect(() => setText(serialized), [serialized])
  return (
    <textarea
      aria-label={label}
      className={`${fieldClass} mt-1`}
      rows={2}
      value={text}
      onChange={(e) => setText(e.target.value)}
      onBlur={() =>
        onChange([
          ...new Set(
            text
              .split(/[,，\n]/)
              .map((v) => v.trim())
              .filter(Boolean)
          ),
        ])
      }
    />
  )
}

function AdapterPreview({
  protocol,
  model,
  registry,
  channelIds,
}: {
  protocol: VideoProtocol
  model: string
  registry?: AdapterRegistry
  channelIds?: number[]
}) {
  const { t } = useTranslation()
  const [input, setInput] = useState(
    JSON.stringify(
      {
        model,
        prompt: '镜头缓慢推进',
        duration: protocol.capabilities?.duration.default ?? 5,
        resolution: protocol.capabilities?.resolutions?.[0] ?? '720p',
      },
      null,
      2
    )
  )
  const [channelId, setChannelId] = useState(channelIds?.[0] ?? 0)
  const [response, setResponse] = useState(
    '{"id":"example-task","status":"completed","url":"https://example.com/video.mp4"}'
  )
  const preview = useMutation({
    mutationFn: async () => {
      const r = await api.post('/api/video-adapters/preview', {
        protocol,
        registry,
        channel_id: channelIds?.includes(channelId)
          ? channelId
          : channelIds?.[0],
        input: JSON.parse(input),
        response: JSON.parse(response),
      })
      if (!r.data.success) throw new Error(r.data.message)
      return r.data.data as unknown
    },
  })
  return (
    <div className='bg-muted/40 space-y-3 rounded-xl p-4'>
      {channelIds && (
        <label className='block text-sm'>
          {t('Preview channel')}
          <select
            className={fieldClass}
            value={channelIds.includes(channelId) ? channelId : channelIds[0]}
            onChange={(e) => setChannelId(Number(e.target.value))}
          >
            {channelIds.map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
          </select>
        </label>
      )}
      <div className='grid gap-3 lg:grid-cols-2'>
        <label className='text-sm'>
          {t('Request example')}
          <textarea
            className={`${fieldClass} mt-1 font-mono`}
            rows={7}
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
        </label>
        <label className='text-sm'>
          {t('Response example')}
          <textarea
            className={`${fieldClass} mt-1 font-mono`}
            rows={7}
            value={response}
            onChange={(e) => setResponse(e.target.value)}
          />
        </label>
      </div>
      <button
        type='button'
        className={buttonClass}
        disabled={preview.isPending}
        onClick={() => preview.mutate()}
      >
        {t('Preview conversion')}
      </button>
      {preview.isError ? (
        <p role='alert' className='text-destructive text-sm'>
          {preview.error.message}
        </p>
      ) : null}
      {preview.data ? (
        <pre className='max-h-96 overflow-auto text-xs break-all whitespace-pre-wrap'>
          {JSON.stringify(preview.data, null, 2)}
        </pre>
      ) : null}
    </div>
  )
}
