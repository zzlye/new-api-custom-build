import { nanoid } from 'nanoid'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  VideoCapabilities,
  VideoParameter,
} from '@/features/channels/components/video-protocol-form'

const inputClass =
  'mt-1 w-full min-w-0 rounded-lg border bg-background px-3 py-2 text-sm'
const split = (text: string) =>
  text
    .split(/[,，\n]/)
    .map((s) => s.trim())
    .filter(Boolean)

export function CapabilityEditor({
  value,
  onChange,
  combination = false,
}: {
  value?: VideoCapabilities
  onChange: (next: VideoCapabilities) => void
  combination?: boolean
}) {
  const { t } = useTranslation()
  const c = value ?? { duration: {} }
  // 独立行标识让编辑参数名或删除前一行时保持焦点与输入内容。
  const [parameterIds, setParameterIds] = useState(() =>
    (c.parameters ?? []).map(() => nanoid())
  )
  const [combinationIds, setCombinationIds] = useState(() =>
    (c.combinations ?? []).map(() => nanoid())
  )
  useEffect(
    () =>
      setParameterIds((ids) =>
        (c.parameters ?? []).map((_, i) => ids[i] ?? nanoid())
      ),
    [c.parameters]
  )
  useEffect(
    () =>
      setCombinationIds((ids) =>
        (c.combinations ?? []).map((_, i) => ids[i] ?? nanoid())
      ),
    [c.combinations]
  )
  const update = (patch: Partial<VideoCapabilities>) =>
    onChange({ ...c, ...patch })
  const parameter = (index: number, patch: Partial<VideoParameter>) =>
    update({
      parameters: c.parameters?.map((p, i) =>
        i === index ? { ...p, ...patch } : p
      ),
    })
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Leave undocumented limits empty. Empty does not mean verified support.'
        )}
      </p>
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
        {(['min', 'max', 'default'] as const).map((key) => (
          <label key={key} className='text-sm'>
            {t(`video.duration.${key}`)}
            <input
              className={inputClass}
              type='number'
              min='1'
              max='3600'
              value={c.duration[key] ?? ''}
              onChange={(e) =>
                update({
                  duration: {
                    ...c.duration,
                    [key]: e.target.value ? Number(e.target.value) : undefined,
                  },
                })
              }
            />
          </label>
        ))}
        <label className='text-sm'>
          {t('Allowed durations')}
          <ListInput
            value={c.duration.values?.join(',') ?? ''}
            onChange={(text) =>
              update({
                duration: { ...c.duration, values: split(text).map(Number) },
              })
            }
          />
        </label>
        {(['resolutions', 'aspect_ratios'] as const).map((key) => (
          <label key={key} className='text-sm'>
            {t(`video.capability.${key}`)}
            <ListInput
              value={c[key]?.join(',') ?? ''}
              onChange={(text) => update({ [key]: split(text) })}
            />
          </label>
        ))}
        {!combination &&
          (['image_limit', 'video_limit', 'audio_limit'] as const).map(
            (key) => (
              <label key={key} className='text-sm'>
                {t(`video.capability.${key}`)}
                <input
                  className={inputClass}
                  type='number'
                  min='0'
                  max='128'
                  value={c[key] ?? ''}
                  onChange={(e) =>
                    update({
                      [key]: e.target.value
                        ? Number(e.target.value)
                        : undefined,
                    })
                  }
                />
              </label>
            )
          )}
        {!combination && (
          <>
            <label className='text-sm'>
              {t('Audio generation capability')}
              <select
                className={inputClass}
                value={
                  c.generate_audio === undefined ? '' : String(c.generate_audio)
                }
                onChange={(e) =>
                  update({
                    generate_audio:
                      e.target.value === ''
                        ? undefined
                        : e.target.value === 'true',
                  })
                }
              >
                <option value=''>{t('Unknown')}</option>
                <option value='true'>{t('Supported')}</option>
                <option value='false'>{t('Unsupported')}</option>
              </select>
            </label>
            <label className='text-sm'>
              {t('Accepted media sources')}
              <select
                className={inputClass}
                value={c.media_transport ?? 'either'}
                onChange={(e) => update({ media_transport: e.target.value })}
              >
                <option value='either'>{t('URL or inline media')}</option>
                <option value='url'>{t('Public URLs only')}</option>
              </select>
            </label>
          </>
        )}
      </div>
      <fieldset className='flex flex-wrap gap-4'>
        <legend className='mb-2 text-sm'>{t('Generation modes')}</legend>
        {['text', 'references', 'frames'].map((mode) => (
          <label key={mode} className='flex items-center gap-2 text-sm'>
            <input
              type='checkbox'
              checked={c.modes?.includes(mode) ?? false}
              onChange={(e) =>
                update({
                  modes: e.target.checked
                    ? [...(c.modes ?? []), mode]
                    : c.modes?.filter((v) => v !== mode),
                })
              }
            />
            {t(`video.mode.${mode}`)}
          </label>
        ))}
      </fieldset>
      {!combination && (
        <>
          <div className='grid gap-2 sm:grid-cols-2'>
            {(
              [
                'audio_requires_visual',
                'video_requires_image',
                'first_frame_with_video',
                'prompt_optional_with_image',
              ] as const
            ).map((key) => (
              <label key={key} className='flex items-center gap-2 text-sm'>
                <input
                  type='checkbox'
                  checked={c[key] ?? false}
                  onChange={(e) => update({ [key]: e.target.checked })}
                />
                {t(`video.capability.${key}`)}
              </label>
            ))}
          </div>
          <div className='space-y-3'>
            <h4 className='font-medium'>{t('Custom video parameters')}</h4>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Map custom parameter values from extra_parameters.KEY to the upstream field.'
              )}
            </p>
            {(c.parameters ?? []).map((p, i) => (
              <div
                key={parameterIds[i] ?? `parameter-${p.key}`}
                className='grid gap-3 rounded-xl border p-3 sm:grid-cols-2 lg:grid-cols-3'
              >
                <label className='text-xs'>
                  {t('Parameter key')}
                  <input
                    className={inputClass}
                    value={p.key}
                    onChange={(e) => parameter(i, { key: e.target.value })}
                  />
                </label>
                <label className='text-xs'>
                  {t('Display name')}
                  <input
                    className={inputClass}
                    value={p.label}
                    onChange={(e) => parameter(i, { label: e.target.value })}
                  />
                </label>
                <label className='text-xs'>
                  {t('Parameter type')}
                  <select
                    className={inputClass}
                    value={p.type}
                    onChange={(e) =>
                      parameter(i, {
                        type: e.target.value as VideoParameter['type'],
                        default: undefined,
                        options: [],
                      })
                    }
                  >
                    {['string', 'integer', 'number', 'boolean'].map((v) => (
                      <option key={v} value={v}>
                        {t(`video.parameter.${v}`)}
                      </option>
                    ))}
                  </select>
                </label>
                <label className='text-xs'>
                  {t('Default value')}
                  {p.type === 'boolean' ? (
                    <select
                      className={inputClass}
                      value={p.default === undefined ? '' : String(p.default)}
                      onChange={(e) =>
                        parameter(i, {
                          default:
                            e.target.value === ''
                              ? undefined
                              : e.target.value === 'true',
                        })
                      }
                    >
                      <option value=''>{t('Unknown')}</option>
                      <option value='true'>true</option>
                      <option value='false'>false</option>
                    </select>
                  ) : (
                    <input
                      className={inputClass}
                      value={p.default === undefined ? '' : String(p.default)}
                      onChange={(e) => {
                        const text = e.target.value
                        parameter(i, {
                          default: text
                            ? parameterValue(p.type, text)
                            : undefined,
                        })
                      }}
                    />
                  )}
                </label>
                <label className='text-xs'>
                  {t('Allowed values')}
                  <ListInput
                    value={p.options?.join(',') ?? ''}
                    onChange={(text) =>
                      parameter(i, {
                        options: split(text).map((v) =>
                          parameterValue(p.type, v)
                        ),
                      })
                    }
                  />
                </label>
                {(['min', 'max'] as const).map((key) => (
                  <label key={key} className='text-xs'>
                    {t(key === 'min' ? 'Minimum' : 'Maximum')}
                    <input
                      className={inputClass}
                      type='number'
                      step='any'
                      value={p[key] ?? ''}
                      onChange={(e) =>
                        parameter(i, {
                          [key]: e.target.value
                            ? Number(e.target.value)
                            : undefined,
                        })
                      }
                    />
                  </label>
                ))}
                <label className='flex items-center gap-2 text-sm'>
                  <input
                    type='checkbox'
                    checked={p.editable}
                    onChange={(e) =>
                      parameter(i, { editable: e.target.checked })
                    }
                  />
                  {t('Allow workshop users to edit')}
                </label>
                <label className='flex items-center gap-2 text-sm'>
                  <input
                    type='checkbox'
                    checked={p.required ?? false}
                    onChange={(e) =>
                      parameter(i, { required: e.target.checked })
                    }
                  />
                  {t('Required')}
                </label>
                <button
                  type='button'
                  className='text-destructive justify-self-start text-sm underline'
                  onClick={() => {
                    setParameterIds((ids) => ids.filter((_, j) => j !== i))
                    update({
                      parameters: c.parameters?.filter(
                        (_, index) => index !== i
                      ),
                    })
                  }}
                >
                  {t('Remove parameter')}
                </button>
              </div>
            ))}
            <button
              type='button'
              className='rounded-lg border px-3 py-2 text-sm'
              onClick={() => {
                setParameterIds((ids) => [...ids, nanoid()])
                update({
                  parameters: [
                    ...(c.parameters ?? []),
                    { key: '', label: '', type: 'string', editable: true },
                  ],
                })
              }}
            >
              {t('Add parameter')}
            </button>
          </div>
          <div className='space-y-3'>
            <h4>{t('Compatible combinations')}</h4>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Each row is a supported combination. Leave empty if all declared options can be combined.'
              )}
            </p>
            {(c.combinations ?? []).map((row, i) => (
              <div
                className='rounded-xl border p-3'
                key={combinationIds[i] ?? JSON.stringify(row)}
              >
                <CapabilityEditor
                  combination
                  value={row}
                  onChange={(next) =>
                    update({
                      combinations: c.combinations?.map((v, j) =>
                        j === i
                          ? {
                              duration: next.duration,
                              resolutions: next.resolutions,
                              aspect_ratios: next.aspect_ratios,
                              modes: next.modes,
                            }
                          : v
                      ),
                    })
                  }
                />
                <button
                  type='button'
                  className='text-sm underline'
                  onClick={() => {
                    setCombinationIds((ids) => ids.filter((_, j) => j !== i))
                    update({
                      combinations: c.combinations?.filter((_, j) => j !== i),
                    })
                  }}
                >
                  {t('Remove')}
                </button>
              </div>
            ))}
            <button
              type='button'
              className='rounded-lg border px-3 py-2 text-sm'
              onClick={() => {
                setCombinationIds((ids) => [...ids, nanoid()])
                update({
                  combinations: [...(c.combinations ?? []), { duration: {} }],
                })
              }}
            >
              {t('Add combination')}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

// 输入期间保留分隔符，失焦再保存列表，避免连续输入被受控值打断。
function ListInput({
  value,
  onChange,
}: {
  value: string
  onChange: (text: string) => void
}) {
  const [text, setText] = useState(value)
  useEffect(() => setText(value), [value])
  return (
    <input
      className={inputClass}
      value={text}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => onChange(text)}
    />
  )
}

function parameterValue(type: VideoParameter['type'], text: string) {
  if (type === 'string') {
    return text
  }
  if (type === 'boolean') {
    return text === 'true'
  }
  return Number(text)
}
