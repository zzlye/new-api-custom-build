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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { parseHttpStatusCodeRules } from '@/lib/http-status-code-rules'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { updateSystemOption } from '../api'
import { SettingsCard } from '../components/settings-card'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  createAsyncMediaRetrySchema,
  type AsyncMediaRetryValues,
} from './async-media-retry-form'

type Channel = { id: number; name: string; status: number }

export function AsyncMediaRetrySection(props: { value: string }) {
  const { t } = useTranslation()
  const root = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const schema = useMemo(() => createAsyncMediaRetrySchema(t), [t])
  const config = useMemo(() => {
    try {
      const raw = JSON.parse(props.value)
      const config = schema.parse({
        ...raw,
        // 旧配置缺失字段时展示为0，不自动增加付费提交次数。
        same_channel_retries: raw.same_channel_retries ?? 0,
        channel_status_codes: raw.channel_status_codes ?? {},
        selected_channels_only: raw.selected_channels_only ?? false,
      })
      // 旧配置中的区间也逐个展开，避免保存前后出现两种错误码显示方式。
      return {
        ...config,
        status_codes:
          config.status_codes &&
          parseHttpStatusCodeRules(config.status_codes, { expandRanges: true })
            .normalized,
        channel_status_codes: Object.fromEntries(
          Object.entries(config.channel_status_codes).map(([id, codes]) => [
            id,
            parseHttpStatusCodeRules(codes, { expandRanges: true }).normalized,
          ])
        ),
      }
    } catch {
      return null
    }
  }, [props.value, schema])
  if (!config) {
    return <ErrorState title={t('Failed to load settings')} />
  }
  return root ? <AsyncMediaRetryEditor defaults={config} /> : null
}

function AsyncMediaRetryEditor(props: { defaults: AsyncMediaRetryValues }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const schema = useMemo(() => createAsyncMediaRetrySchema(t), [t])
  const form = useForm<AsyncMediaRetryValues>({
    resolver: zodResolver(schema),
    defaultValues: props.defaults,
  })
  const { dirtyFields } = form.formState
  const [search, setSearch] = useState('')
  const [selectionError, setSelectionError] = useState(false)
  // 后台刷新不覆盖尚未保存的草稿，渠道搜索也不改变隐藏行的勾选。
  useEffect(() => {
    form.reset(props.defaults, { keepDirtyValues: true })
  }, [props.defaults, form])
  const selected = useWatch({ control: form.control, name: 'channel_ids' })
  const channels = useQuery({
    queryKey: ['async-media-retry-channels'],
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const response = await api.get<{ success: boolean; data: Channel[] }>(
        '/api/option/async_media_retry_channels'
      )
      return requireServerSuccess(response.data).data || []
    },
  })
  const query = search.trim().toLowerCase()
  // 旧策略只在尚未编辑的渠道行填入原值，保存后不再保留通用错误码。
  useEffect(() => {
    if (!channels.data) return
    for (const channel of channels.data) {
      const name = `channel_status_codes.${channel.id}` as const
      if (
        form.getValues(name) === undefined ||
        (props.defaults.status_codes &&
          !form.getValues(name) &&
          !form.getFieldState(name).isDirty)
      ) {
        form.setValue(name, props.defaults.status_codes ?? '')
      }
    }
    // 将旧版不限渠道策略转成明确的勾选名单，已取消的勾选不会被再次填回。
    if (!form.getValues('selected_channels_only')) {
      if (
        props.defaults.channel_ids.length === 0 &&
        !form.getFieldState('channel_ids').isDirty
      ) {
        form.setValue(
          'channel_ids',
          channels.data
            .filter((channel) =>
              form.getValues(`channel_status_codes.${channel.id}`)?.trim()
            )
            .map((channel) => channel.id)
        )
      }
      form.setValue('selected_channels_only', true)
    }
    form.setValue('status_codes', undefined)
  }, [channels.data, props.defaults, form])
  const visible = (channels.data || []).filter(
    (channel) =>
      channel.name.toLowerCase().includes(query) ||
      String(channel.id).includes(query)
  )
  const missing = selected.filter(
    (id) => channels.data && !channels.data.some((channel) => channel.id === id)
  )
  const save = useMutation({
    mutationFn: async (value: AsyncMediaRetryValues) => {
      requireServerSuccess(
        await updateSystemOption({
          key: 'AsyncMediaRetryPolicy',
          value: JSON.stringify(value),
        })
      )
      return value
    },
    onSuccess: (value) => {
      form.reset(value)
      toast.success(t('Settings saved successfully'))
      void client.invalidateQueries({ queryKey: ['system-options'] })
    },
    onError: (error) => handleServerError(error),
  })
  const submit = form.handleSubmit((values) => {
    if (values.enabled && values.channel_ids.length === 0) {
      setSelectionError(true)
      return
    }
    if (values.enabled) {
      const empty = values.channel_ids.filter(
        (id) => !values.channel_status_codes[id]?.trim()
      )
      for (const id of empty) {
        form.setError(`channel_status_codes.${id}`, {
          message: t(
            'Enter HTTP error codes from 400 to 599, such as 500 or 500-503.'
          ),
        })
      }
      if (empty.length > 0) return
    }
    setSelectionError(false)
    save.mutate({
      ...values,
      status_codes: undefined,
      channel_status_codes: Object.fromEntries(
        Object.entries(values.channel_status_codes)
          .filter(([, value]) => value.trim())
          .map(([id, value]) => [
            id,
            parseHttpStatusCodeRules(value, { expandRanges: true }).normalized,
          ])
      ),
      channel_ids: values.channel_ids,
      selected_channels_only: true,
    })
  })
  const setSelected = (ids: number[]) => {
    form.setValue('channel_ids', ids, {
      shouldDirty: true,
      shouldValidate: true,
    })
    setSelectionError(false)
  }
  return (
    <Form {...form}>
      <SettingsPageFormActions
        onSave={submit}
        isSaving={save.isPending}
        isSaveDisabled={!channels.data}
      />
      <SettingsCard title={t('Media task retries')}>
        <fieldset disabled={save.isPending} className='min-w-0 space-y-5'>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between gap-3'>
                <FormLabel>{t('Enable media channel failover')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='same_channel_retries'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Same-channel retry limit')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={5}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Additional attempts on the current channel before switching. Zero skips same-channel retries.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='max_retries'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Channel switch retry limit')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={20}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Maximum additional channels. Same-channel retries do not consume this limit; zero disables channel switching.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Selected channels trigger retries using their own error codes. Any eligible channel can receive failover; unselected channels do not trigger further retries.'
            )}
          </p>
          <div className='min-w-0 space-y-3'>
            <Input
              aria-label={t('Search channels by name or ID')}
              placeholder={t('Search channels by name or ID')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
            {channels.isError ? (
              <ErrorState
                title={t('Failed to load channels')}
                onRetry={() => void channels.refetch()}
              />
            ) : (
              <>
                <div className='flex flex-wrap items-center gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    disabled={channels.isPending}
                    onClick={() =>
                      setSelected([
                        ...new Set([
                          ...selected,
                          ...visible.map((channel) => channel.id),
                        ]),
                      ])
                    }
                  >
                    {t('Select visible channels')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setSelected([])}
                  >
                    {t('Clear selection')}
                  </Button>
                  <span className='text-muted-foreground text-sm'>
                    {t('{{count}} channels selected', {
                      count: selected.length,
                    })}
                  </span>
                </div>
                <ScrollArea
                  className='h-72 rounded-lg border'
                  aria-label={t('Retry channel list')}
                >
                  <div className='divide-y'>
                    {channels.isPending && (
                      <p className='p-4 text-sm'>{t('Loading...')}</p>
                    )}
                    {!channels.isPending && visible.length === 0 && (
                      <p className='p-4 text-sm'>{t('No channels found')}</p>
                    )}
                    <div className='bg-muted/40 hidden grid-cols-[minmax(0,1fr)_minmax(160px,1fr)] gap-3 px-3 py-2 text-sm font-medium sm:grid'>
                      <span>{t('Channel')}</span>
                      <span>{t('Retry HTTP error codes')}</span>
                    </div>
                    {visible.map((channel) => (
                      <div
                        key={channel.id}
                        className='hover:bg-muted/40 grid min-h-16 grid-cols-1 items-start gap-3 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(160px,1fr)]'
                      >
                        <div className='flex min-w-0 items-center gap-3 pt-2'>
                          <Checkbox
                            aria-label={`${channel.name} #${channel.id}`}
                            checked={selected.includes(channel.id)}
                            onCheckedChange={(checked) =>
                              setSelected(
                                checked
                                  ? [...new Set([...selected, channel.id])]
                                  : selected.filter((id) => id !== channel.id)
                              )
                            }
                          />
                          <span className='min-w-0 flex-1 break-words'>
                            {channel.name}
                          </span>
                          <span className='shrink-0 font-mono text-xs'>
                            #{channel.id}
                          </span>
                          {channel.status !== 1 && (
                            <span className='text-muted-foreground text-xs'>
                              {t('Disabled')}
                            </span>
                          )}
                        </div>
                        <FormField
                          control={form.control}
                          name={`channel_status_codes.${channel.id}`}
                          defaultValue=''
                          render={({ field }) => (
                            <FormItem className='min-w-0'>
                              <FormLabel className='sr-only'>
                                {t('Retry HTTP error codes')} — {channel.name} #
                                {channel.id}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  {...field}
                                  value={field.value ?? ''}
                                  placeholder={t('Not configured')}
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      </div>
                    ))}
                  </div>
                </ScrollArea>
                {missing.length > 0 && (
                  <p className='text-muted-foreground text-sm'>
                    {t('Selected channels no longer available')}:{' '}
                    {missing.join(', ')}{' '}
                    <Button
                      type='button'
                      size='sm'
                      variant='ghost'
                      onClick={() =>
                        setSelected(
                          selected.filter((id) => !missing.includes(id))
                        )
                      }
                    >
                      {t('Remove')}
                    </Button>
                  </p>
                )}
              </>
            )}
            {selectionError && (
              <p role='alert' className='text-destructive text-sm'>
                {t('Select at least one channel before enabling failover.')}
              </p>
            )}
            {form.formState.errors.enabled?.message && (
              <p role='alert' className='text-destructive text-sm'>
                {form.formState.errors.enabled.message}
              </p>
            )}
          </div>
          {Object.keys(dirtyFields).length > 0 && (
            <span className='sr-only'>{t('Unsaved changes')}</span>
          )}
        </fieldset>
      </SettingsCard>
    </Form>
  )
}
