import { nanoid } from 'nanoid'
import { useEffect, useState } from 'react'

export interface VideoField {
  source: string
  target: string
  format: string
  item_key?: string
}

export interface VideoProtocol {
  enabled: boolean
  submit_path: string
  poll_path: string
  poll_method: string
  poll_id_field: string
  content_path: string
  encoding: string
  auth_mode: string
  auth_name: string
  auth_prefix: string
  fields: VideoField[]
  defaults: Record<string, unknown>
  headers: Record<string, string>
  response: {
    id: string
    status: string
    url: string
    error: string
    progress: string
    states: Record<string, string>
  }
}

export interface VideoProtocolCatalog {
  channels: { id: number; name: string }[]
  presets: Record<string, VideoProtocol>
}

interface Props {
  value: VideoProtocol
  onChange: (value: VideoProtocol) => void
  t: (key: string) => string
  disabled?: boolean
}

const inputClass =
  'w-full min-w-0 rounded-lg border bg-transparent px-2 py-1.5 text-sm'
const textFields = [
  ['submit_path', 'Submit path'],
  ['poll_path', 'Poll path'],
  ['poll_id_field', 'Poll ID field'],
  ['content_path', 'Content path'],
  ['auth_name', 'Authentication field'],
  ['auth_prefix', 'Authentication prefix'],
] as const
const responseFields = [
  ['id', 'Task ID path'],
  ['status', 'Task status path'],
  ['url', 'Video URL path'],
  ['error', 'Error message path'],
  ['progress', 'Progress path'],
] as const

// 基础参数用表单编辑；复杂常量和状态规则单独展开，避免日常配置依赖手写整份 JSON。
export function VideoProtocolForm(props: Props) {
  const p = props.value
  const t = props.t
  // 字段名称可以编辑，因此使用独立行标识保持焦点，删除一行不复用另一行的控件。
  const [rowIds, setRowIds] = useState(() => p.fields.map(() => nanoid()))
  useEffect(
    () => setRowIds((ids) => p.fields.map((_, i) => ids[i] ?? nanoid())),
    [p.fields]
  )
  function updateField(index: number, patch: Partial<VideoField>) {
    props.onChange({
      ...p,
      fields: p.fields.map((field, i) =>
        i === index ? { ...field, ...patch } : field
      ),
    })
  }
  return (
    <fieldset disabled={props.disabled} className='min-w-0 space-y-4'>
      <label className='flex items-center gap-2 text-sm'>
        <input
          type='checkbox'
          checked={p.enabled}
          onChange={(e) => props.onChange({ ...p, enabled: e.target.checked })}
        />
        {t('Enable video protocol')}
      </label>
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
        {textFields.map(([key, label]) => (
          <label key={key} className='min-w-0 space-y-1 text-sm'>
            <span>{t(label)}</span>
            <input
              className={inputClass}
              value={p[key]}
              onChange={(e) => props.onChange({ ...p, [key]: e.target.value })}
            />
          </label>
        ))}
        {(
          [
            ['encoding', 'Request encoding', ['json', 'form', 'multipart']],
            ['poll_method', 'Poll method', ['GET', 'POST']],
            ['auth_mode', 'Authentication mode', ['header', 'query', 'none']],
          ] as const
        ).map(([key, label, options]) => (
          <label key={key} className='space-y-1 text-sm'>
            <span>{t(label)}</span>
            <select
              className={inputClass}
              value={p[key]}
              onChange={(e) => props.onChange({ ...p, [key]: e.target.value })}
            >
              {options.map((option) => (
                <option key={option}>{option}</option>
              ))}
            </select>
          </label>
        ))}
      </div>
      <p className='text-xs opacity-70'>
        {t(
          'Paths are relative to the channel URL. Use {id} for the task ID. Leave content path empty to download the result URL.'
        )}
      </p>
      <div className='space-y-2'>
        <h4 className='text-sm font-semibold'>
          {t('Video parameter mapping')}
        </h4>
        {p.fields.map((field, index) => (
          <div
            key={rowIds[index] ?? field.source}
            className='grid min-w-0 grid-cols-2 gap-2 rounded-lg border p-2'
          >
            <label className='min-w-0 text-xs'>
              {t('Input field')}
              <input
                className={inputClass}
                value={field.source}
                onChange={(e) => updateField(index, { source: e.target.value })}
              />
            </label>
            <label className='min-w-0 text-xs'>
              {t('Upstream field')}
              <input
                className={inputClass}
                value={field.target}
                onChange={(e) => updateField(index, { target: e.target.value })}
              />
            </label>
            <label className='min-w-0 text-xs'>
              {t('Value format')}
              <select
                className={inputClass}
                value={field.format}
                onChange={(e) => updateField(index, { format: e.target.value })}
              >
                {(
                  [
                    'identity',
                    'string',
                    'number',
                    'boolean',
                    'single',
                    'single_object',
                    'objects',
                    'frames',
                  ] as const
                ).map((format) => (
                  <option key={format} value={format}>
                    {t(`video.format.${format}`)}
                  </option>
                ))}
              </select>
            </label>
            <label className='min-w-0 text-xs'>
              {t('Object URL field')}
              <input
                className={inputClass}
                disabled={!['single_object', 'objects'].includes(field.format)}
                value={field.item_key ?? ''}
                placeholder='image_url'
                onChange={(e) =>
                  updateField(index, { item_key: e.target.value })
                }
              />
            </label>
            <button
              type='button'
              className='justify-self-start text-xs underline'
              onClick={() => {
                setRowIds((ids) => ids.filter((_, i) => i !== index))
                props.onChange({
                  ...p,
                  fields: p.fields.filter((_, i) => i !== index),
                })
              }}
            >
              {t('Remove mapping')}
            </button>
          </div>
        ))}
        <button
          type='button'
          className='rounded-lg border px-3 py-1.5 text-sm'
          onClick={() =>
            props.onChange({
              ...p,
              fields: [
                ...p.fields,
                { source: '', target: '', format: 'identity' },
              ],
            })
          }
        >
          {t('Add mapping')}
        </button>
      </div>
      <details className='space-y-3'>
        <summary className='cursor-pointer text-sm font-semibold'>
          {t('Response mapping and extra parameters')}
        </summary>
        <p className='text-xs opacity-70'>
          {t(
            'Use dots for nested fields and | for alternative response paths. Extra parameters cannot replace mapped fields.'
          )}
        </p>
        <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
          {responseFields.map(([key, label]) => (
            <label key={key} className='min-w-0 text-sm'>
              {t(label)}
              <input
                className={inputClass}
                value={p.response[key]}
                onChange={(e) =>
                  props.onChange({
                    ...p,
                    response: { ...p.response, [key]: e.target.value },
                  })
                }
              />
            </label>
          ))}
        </div>
        <JSONField
          label={t('Extra parameters')}
          invalidMessage={t('Enter a JSON object')}
          value={p.defaults}
          onChange={(defaults) => props.onChange({ ...p, defaults })}
        />
        <JSONField
          label={t('Extra request headers')}
          invalidMessage={t('Enter a JSON object')}
          value={p.headers ?? {}}
          onChange={(headers) =>
            props.onChange({ ...p, headers: headers as Record<string, string> })
          }
        />
        <JSONField
          label={t('Task state mapping')}
          invalidMessage={t('Enter a JSON object')}
          value={p.response.states}
          onChange={(states) =>
            props.onChange({
              ...p,
              response: {
                ...p.response,
                states: states as Record<string, string>,
              },
            })
          }
        />
      </details>
    </fieldset>
  )
}

function JSONField(props: {
  label: string
  invalidMessage: string
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
}) {
  const serialized = JSON.stringify(props.value, null, 2)
  const [text, setText] = useState(serialized)
  useEffect(() => setText(serialized), [serialized])
  return (
    <label className='block text-sm'>
      {props.label}
      <textarea
        className={`${inputClass} mt-1 font-mono`}
        rows={5}
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          try {
            const value: unknown = JSON.parse(e.target.value)
            if (!value || typeof value !== 'object' || Array.isArray(value)) {
              throw new Error('JSON object required')
            }
            props.onChange(value as Record<string, unknown>)
            e.target.setCustomValidity('')
          } catch {
            e.target.setCustomValidity(props.invalidMessage)
          }
        }}
      />
    </label>
  )
}
