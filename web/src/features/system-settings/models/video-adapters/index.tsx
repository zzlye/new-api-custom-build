import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { PaginationState } from '@tanstack/react-table'
import { Plus } from 'lucide-react'
import { nanoid } from 'nanoid'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  VideoProtocolForm,
  type VideoProtocol,
} from '@/features/channels/components/video-protocol-form'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CapabilityEditor } from './capability-editor'
import { AdapterConfigTransfer } from './config-transfer'
import { AdapterRulesTable } from './rules-table'

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
export function VideoAdaptersSection() {
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  return root ? <VideoAdaptersManager /> : null
}

function VideoAdaptersManager() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [draft, setDraft] = useState<AdapterRegistry | null>(null)
  const [search, setSearch] = useState('')
  const [activeView, setActiveView] = useState('rules')
  const [editingRuleId, setEditingRuleId] = useState('')
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })
  const [documentChannels, setDocumentChannels] = useState<number[]>([])
  const [activeTemplate, setActiveTemplate] = useState('')
  const [message, setMessage] = useState('')
  const [importOpen, setImportOpen] = useState(false)
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
  const appendRule = (rule: AdapterRule) => {
    setDraft({
      ...draft,
      rules: [...draft.rules, rule],
    })
    setSearch('')
    setActiveView('rules')
    setPagination((previous) => ({
      ...previous,
      pageIndex: Math.floor(draft.rules.length / previous.pageSize),
    }))
    setEditingRuleId(rule.id)
  }
  const addRule = () =>
    appendRule({
      id: nanoid(),
      enabled: true,
      channel_ids: [],
      models: [],
      template_id: draft.templates[0]?.id ?? '',
    })
  const duplicateRule = (rule: AdapterRule) =>
    appendRule({ ...structuredClone(rule), id: nanoid(), enabled: false })
  const visible = draft.rules.filter((r) =>
    `${r.models.join(' ')} ${r.template_id} ${draft.templates.find((item) => item.id === r.template_id)?.name ?? ''} ${r.channel_ids.map((id) => `${id} ${channels.find((c) => c.id === id)?.name ?? ''}`).join(' ')}`
      .toLowerCase()
      .includes(search.trim().toLowerCase())
  )
  const template = draft.templates.find((p) => p.id === activeTemplate)
  const editingRule = draft.rules.find((rule) => rule.id === editingRuleId)
  return (
    <form
      className='min-w-0 space-y-5'
      onSubmit={(e) => {
        e.preventDefault()
        if (importOpen || save.isPending) return
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
          <Button
            type='submit'
            disabled={save.isPending || !dirty || importOpen}
          >
            {save.isPending ? t('Saving...') : t('Publish rules')}
          </Button>
        </div>
      </div>
      {save.isError ? (
        <p role='alert' className='text-destructive'>
          {save.error.message}
        </p>
      ) : null}
      {message ? <p role='status'>{message}</p> : null}
      <AdapterConfigTransfer
        registry={draft}
        channels={channels}
        disabled={save.isPending}
        onOpenChange={setImportOpen}
        onImport={(next) => {
          setDraft(next)
          setSearch('')
          setActiveView('rules')
          setEditingRuleId('')
          setPagination((previous) => ({ ...previous, pageIndex: 0 }))
          setMessage(
            t('Configuration added to draft. Review it and publish when ready.')
          )
        }}
      />
      <Tabs
        value={activeView}
        onValueChange={(value) => setActiveView(String(value))}
      >
        <TabsList className='h-auto w-full flex-wrap justify-start gap-1 sm:w-fit'>
          <TabsTrigger value='rules'>{t('Rules')}</TabsTrigger>
          <TabsTrigger value='templates'>
            {t('Manage reusable templates')}
          </TabsTrigger>
          <TabsTrigger value='bulk'>
            {t('Bind documented models in bulk')}
          </TabsTrigger>
        </TabsList>
        <TabsContent value='rules' className='min-w-0 space-y-4 pt-3'>
          <div className='flex flex-wrap gap-2'>
            <Input
              aria-label={t('Search video adapters')}
              className={`${fieldClass} sm:max-w-sm`}
              placeholder={t('Search models or channels')}
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setPagination((previous) => ({ ...previous, pageIndex: 0 }))
              }}
            />
            <Button
              type='button'
              variant='outline'
              disabled={save.isPending}
              onClick={addRule}
            >
              <Plus />
              {t('Add row')}
            </Button>
          </div>
          <AdapterRulesTable
            rules={visible}
            channels={channels}
            templates={draft.templates}
            pagination={pagination}
            onPaginationChange={setPagination}
            disabled={save.isPending}
            onEdit={(rule) => setEditingRuleId(rule.id)}
            onDuplicate={duplicateRule}
            onDelete={(rule) =>
              setDraft({
                ...draft,
                rules: draft.rules.filter((item) => item.id !== rule.id),
              })
            }
            onEnabledChange={(rule, enabled) =>
              updateRule(rule.id, { enabled })
            }
          />
        </TabsContent>
        <TabsContent value='bulk' className='min-w-0 pt-3'>
          <fieldset disabled={save.isPending} className='min-w-0'>
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
                      Array.from(e.target.selectedOptions, (o) =>
                        Number(o.value)
                      )
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
              <Button
                type='button'
                variant='outline'
                disabled={!documentChannels.length}
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
                    .filter((name) =>
                      draft.templates.some((p) => p.id === name)
                    )
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
              </Button>
            </div>
          </fieldset>
        </TabsContent>
        {/* 仅挂载当前规则的编辑表单，翻页只改变摘要列表，不改动完整草稿。 */}
        <Dialog
          open={Boolean(editingRule)}
          onOpenChange={(open) => {
            if (!open) setEditingRuleId('')
          }}
          title={t('Edit')}
          showCloseButton={false}
          contentClassName='max-h-[90dvh] sm:max-w-5xl'
          footer={
            <Button
              type='button'
              variant='outline'
              onClick={() => setEditingRuleId('')}
            >
              {t('Close')}
            </Button>
          }
        >
          <fieldset
            disabled={save.isPending}
            className='min-w-0 space-y-3 p-3 sm:p-4'
          >
            {(editingRule ? [editingRule] : []).map((rule) => {
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
                <article key={rule.id} className='min-w-0'>
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
                      <NativeSelect
                        className='w-full min-w-0'
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
                      </NativeSelect>
                      <p className='text-muted-foreground text-xs'>
                        {t('Empty model names apply as the channel default.')}
                      </p>
                    </div>
                    <label className='min-w-0 text-sm'>
                      {t('Protocol template')}
                      <NativeSelect
                        className='mt-1 w-full min-w-0'
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
                      </NativeSelect>
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
        </Dialog>
        <TabsContent value='templates' className='min-w-0 pt-3'>
          <fieldset disabled={save.isPending} className='min-w-0'>
            <div className='mt-4 space-y-4'>
              <div className='flex flex-wrap gap-2'>
                <NativeSelect
                  aria-label={t('Edit template')}
                  className='w-full min-w-0 sm:max-w-md'
                  value={activeTemplate}
                  onChange={(e) => setActiveTemplate(e.target.value)}
                >
                  <option value=''>{t('Choose template')}</option>
                  {draft.templates.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </NativeSelect>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => {
                    const source = template ?? draft.templates[0]
                    if (!source) return
                    const copy = {
                      ...structuredClone(source),
                      id: nanoid(),
                      name: `${source.name} ${t('Copy')}`,
                    }
                    setDraft({
                      ...draft,
                      templates: [...draft.templates, copy],
                    })
                    setActiveTemplate(copy.id)
                  }}
                >
                  {t('New template from copy')}
                </Button>
                {template ? (
                  <Button
                    type='button'
                    variant='outline'
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
                  </Button>
                ) : null}
              </div>
              {template ? (
                <div key={template.id} className='space-y-5'>
                  <label className='block text-sm'>
                    {t('Template name')}
                    <Input
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
          </fieldset>
        </TabsContent>
      </Tabs>
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
  const committed = useRef(serialized)
  useEffect(() => {
    if (serialized !== committed.current) {
      committed.current = serialized
      setText(serialized)
    }
  }, [serialized])
  return (
    <Textarea
      aria-label={label}
      className={`${fieldClass} mt-1`}
      rows={2}
      value={text}
      onChange={(e) => {
        const next = e.target.value
        const models = [
          ...new Set(
            next
              .split(/[,，\n]/)
              .map((v) => v.trim())
              .filter(Boolean)
          ),
        ]
        // 立即更新草稿以支持 Esc 关闭，同时保留正在输入的逗号和换行。
        committed.current = models.join(',')
        setText(next)
        onChange(models)
      }}
      onBlur={() => setText(serialized)}
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
          <NativeSelect
            className='w-full min-w-0'
            value={channelIds.includes(channelId) ? channelId : channelIds[0]}
            onChange={(e) => setChannelId(Number(e.target.value))}
          >
            {channelIds.map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
          </NativeSelect>
        </label>
      )}
      <div className='grid gap-3 lg:grid-cols-2'>
        <label className='text-sm'>
          {t('Request example')}
          <Textarea
            className={`${fieldClass} mt-1 font-mono`}
            rows={7}
            value={input}
            onChange={(e) => setInput(e.target.value)}
          />
        </label>
        <label className='text-sm'>
          {t('Response example')}
          <Textarea
            className={`${fieldClass} mt-1 font-mono`}
            rows={7}
            value={response}
            onChange={(e) => setResponse(e.target.value)}
          />
        </label>
      </div>
      <Button
        type='button'
        variant='outline'
        disabled={preview.isPending}
        onClick={() => preview.mutate()}
      >
        {t('Preview conversion')}
      </Button>
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
