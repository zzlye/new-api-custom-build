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
import { useFieldArray, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getGroups } from '@/features/users/api'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { updateSystemOption } from '../api'
import { SettingsCard } from '../components/settings-card'
import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { safeNumberFieldProps } from '../utils/numeric-field'

// 校验上游错误状态码表达式是否由 400-599 的整数或区间组成
function isValidUpstreamStatusCodes(value: string): boolean {
  const normalized = value.replaceAll('，', ',').trim()
  if (!normalized) return true
  const tokens = normalized.split(',')
  for (const raw of tokens) {
    const token = raw.trim()
    if (!token) continue
    const parts = token.split('-')
    if (parts.length === 1) {
      const code = Number(parts[0])
      if (!Number.isInteger(code) || code < 400 || code > 599) return false
    } else if (parts.length === 2) {
      const start = Number(parts[0])
      const end = Number(parts[1])
      if (
        !Number.isInteger(start) ||
        !Number.isInteger(end) ||
        start > end ||
        start < 400 ||
        end > 599
      ) {
        return false
      }
    } else {
      return false
    }
  }
  return true
}

// 与服务端使用相同的路径限制，防止将上游错误原文误存成参数路径。
function isValidLocalParameterRules(value: string): boolean {
  if (new TextEncoder().encode(value).length > 65536) return false
  const lines = value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
  if (lines.length > 1000) return false
  return lines.every((line) => {
    // 模型范围与后端保持一致，避免把某个模型的限制套到整个分组。
    const separator = line.indexOf('&&')
    if (separator >= 0 && /^model\s*=/.test(line)) {
      const modelList = line.slice(0, separator).split('=').slice(1).join('=')
      const models = modelList.split(',').map((model) => model.trim())
      if (
        models.length > 64 ||
        models.some(
          (model) =>
            model.length > 256 ||
            !/^[A-Za-z0-9][A-Za-z0-9_./:+-]*\*?$/.test(model)
        )
      ) {
        return false
      }
      line = line.slice(separator + 2).trim()
      if (line.includes('&&')) return false
    }
    const path = line.split('=', 1)[0].trim()
    return (
      path.length <= 256 &&
      /^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*$/.test(path)
    )
  })
}

// 本地参数校验与错误拦截策略表单结构定义
const policySchema = z.object({
  enabled: z.boolean(),
  check_structure: z.boolean().default(true),
  check_media: z.boolean().default(true),
  check_generation_config: z.boolean().default(true),
  blocked_keywords: z.string().default(''),
  local_parameter_rules: z
    .string()
    .default('')
    .refine(isValidLocalParameterRules, {
      message:
        'Enter one parameter path or path=value per line (up to 1000 lines, 64 KB).',
    }),
  upstream_cooldown_seconds: z
    .number({ error: 'Enter an integer cooldown from 0 to 86400.' })
    .int({ error: 'Enter an integer cooldown from 0 to 86400.' })
    .min(0, { error: 'Enter an integer cooldown from 0 to 86400.' })
    .max(86400, { error: 'Enter an integer cooldown from 0 to 86400.' })
    .default(60),
  upstream_status_codes: z
    .string()
    .default('400,524')
    .refine(isValidUpstreamStatusCodes, {
      message: 'Enter HTTP status codes from 400 to 599.',
    }),
  // 缺失字段保持为空，避免保存其他设置时意外启用错误特征拦截。
  upstream_error_keywords: z.string().default(''),
  rules: z.array(
    z.object({
      group: z.string().min(1),
      enabled: z.boolean(),
      status_code: z
        .number({ error: 'Enter an integer HTTP status from 400 to 599.' })
        .int({ error: 'Enter an integer HTTP status from 400 to 599.' })
        .min(400, { error: 'Enter an integer HTTP status from 400 to 599.' })
        .max(599, { error: 'Enter an integer HTTP status from 400 to 599.' }),
    })
  ),
})
type Policy = z.output<typeof policySchema>
type PolicyInput = z.input<typeof policySchema>

// 本地参数校验与错误拦截设置入口组件，仅超级管理员可见
export function LocalRequestValidationSection(props: { value: string }) {
  const { t } = useTranslation()
  const root = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const defaults = useMemo(() => {
    try {
      return policySchema.parse(JSON.parse(props.value))
    } catch {
      return null
    }
  }, [props.value])
  if (!root) return null
  if (!defaults) return <ErrorState title={t('Failed to load settings')} />
  return <LocalRequestValidationEditor defaults={defaults} />
}

// 本地参数校验与错误拦截策略编辑器
function LocalRequestValidationEditor(props: { defaults: Policy }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [search, setSearch] = useState('')
  const form = useForm<PolicyInput, unknown, Policy>({
    resolver: zodResolver(policySchema),
    defaultValues: props.defaults,
  })
  const { dirtyFields } = form.formState
  const { fields, append } = useFieldArray({
    control: form.control,
    name: 'rules',
  })
  const values = useWatch({ control: form.control, name: 'rules' })
  const groups = useQuery({
    queryKey: ['local-parameter-validation-groups'],
    queryFn: async () => requireServerSuccess(await getGroups()).data ?? [],
    refetchOnWindowFocus: false,
  })
  useEffect(() => {
    form.reset(props.defaults, { keepDirtyValues: true })
  }, [props.defaults, form])
  useEffect(() => {
    if (!groups.data) return
    // 使用数组索引注册表单，包含点号的分组名称不会被误解成嵌套字段路径。
    const existing = new Set(form.getValues('rules').map((rule) => rule.group))
    const added = groups.data.filter((group) => !existing.has(group)).sort()
    if (added.length) {
      append(
        added.map((group) => ({ group, enabled: false, status_code: 400 })),
        { shouldFocus: false }
      )
    }
  }, [groups.data, props.defaults, form, append])
  const save = useMutation({
    mutationFn: async (policy: Policy) => {
      requireServerSuccess(
        await updateSystemOption({
          key: 'LocalRequestValidationPolicy',
          value: JSON.stringify(policy),
        })
      )
      return policy
    },
    onSuccess: (policy) => {
      form.reset(policy)
      toast.success(t('Settings updated successfully'))
      void client.invalidateQueries({ queryKey: ['system-options'] })
    },
    onError: (error) => handleServerError(error),
  })
  const submit = form.handleSubmit((policy) => {
    if (policy.enabled && !policy.rules.some((rule) => rule.enabled)) {
      form.setError('enabled', { message: t('Select at least one group.') })
      return
    }
    if (
      policy.enabled &&
      !policy.check_structure &&
      !policy.check_media &&
      !policy.check_generation_config &&
      !policy.blocked_keywords.trim() &&
      !policy.local_parameter_rules.trim() &&
      policy.upstream_cooldown_seconds <= 0
    ) {
      form.setError('enabled', {
        message: t('Enable at least one interception rule.'),
      })
      return
    }
    save.mutate(policy)
  })
  const query = search.trim().toLowerCase()
  const visible = fields
    .map((row, index) => ({ ...row, index }))
    .filter((row) => row.group.toLowerCase().includes(query))

  return (
    <Form {...form}>
      <SettingsCard title={t('Local parameter validation')}>
        <SettingsPageFormActions onSave={submit} isSaving={save.isPending} />
        <fieldset disabled={save.isPending} className='min-w-0 space-y-4'>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Enable local parameter validation')}
                  </FormLabel>
                  <FormMessage />
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div className='grid gap-3 sm:grid-cols-3'>
            <FormField
              control={form.control}
              name='check_structure'
              render={({ field }) => (
                <FormItem className='flex items-center gap-2 space-y-0 rounded-md border p-3'>
                  <FormControl>
                    <Checkbox
                      checked={field.value ?? true}
                      onCheckedChange={(checked) =>
                        field.onChange(Boolean(checked))
                      }
                    />
                  </FormControl>
                  <FormLabel className='cursor-pointer font-normal'>
                    {t('Validate request structure')}
                  </FormLabel>
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='check_media'
              render={({ field }) => (
                <FormItem className='flex items-center gap-2 space-y-0 rounded-md border p-3'>
                  <FormControl>
                    <Checkbox
                      checked={field.value ?? true}
                      onCheckedChange={(checked) =>
                        field.onChange(Boolean(checked))
                      }
                    />
                  </FormControl>
                  <FormLabel className='cursor-pointer font-normal'>
                    {t('Validate media payloads')}
                  </FormLabel>
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='check_generation_config'
              render={({ field }) => (
                <FormItem className='flex items-center gap-2 space-y-0 rounded-md border p-3'>
                  <FormControl>
                    <Checkbox
                      checked={field.value ?? true}
                      onCheckedChange={(checked) =>
                        field.onChange(Boolean(checked))
                      }
                    />
                  </FormControl>
                  <FormLabel className='cursor-pointer font-normal'>
                    {t('Validate generation parameters')}
                  </FormLabel>
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='upstream_cooldown_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Upstream error cooldown (seconds)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={86400}
                      step={1}
                      {...safeNumberFieldProps({
                        ...field,
                        value: field.value ?? 60,
                      })}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='upstream_status_codes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Intercepted upstream status codes')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      placeholder='400,524'
                      value={field.value ?? ''}
                      onChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='upstream_error_keywords'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Intercepted upstream error keywords')}
                  </FormLabel>
                  <FormControl>
                    <Textarea
                      rows={3}
                      value={field.value ?? ''}
                      onChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='blocked_keywords'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Blocked request keywords')}</FormLabel>
                  <FormControl>
                    <Textarea
                      rows={3}
                      value={field.value ?? ''}
                      onChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='local_parameter_rules'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Local parameter rules')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={3}
                    placeholder='model=gemini-3.1-pro-preview && generationConfig.thinkingConfig.thinkingLevel=MINIMAL'
                    value={field.value ?? ''}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <Input
            aria-label={t('Search groups')}
            placeholder={t('Search groups')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={groups.isPending || groups.isError}
              onClick={() =>
                fields.forEach((row, index) => {
                  if (row.group.toLowerCase().includes('gemini')) {
                    form.setValue(`rules.${index}.enabled`, true, {
                      shouldDirty: true,
                    })
                  }
                })
              }
            >
              {t('Select Gemini groups')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={groups.isPending || groups.isError}
              onClick={() =>
                visible.forEach((row) =>
                  form.setValue(`rules.${row.index}.enabled`, true, {
                    shouldDirty: true,
                  })
                )
              }
            >
              {t('Select search results')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() =>
                fields.forEach((_, index) =>
                  form.setValue(`rules.${index}.enabled`, false, {
                    shouldDirty: true,
                  })
                )
              }
            >
              {t('Clear selection')}
            </Button>
          </div>
          {groups.isPending && <LoadingState />}
          {groups.isError && (
            <ErrorState onRetry={() => void groups.refetch()} />
          )}
          {groups.isSuccess && (
            <StaticDataTable
              className='max-h-80 overflow-auto'
              data={visible}
              getRowKey={(row) => row.id}
              emptyContent={t('No data')}
              columns={[
                {
                  id: 'group',
                  header: t('Group'),
                  cell: (row) => (
                    <div className='flex min-w-0 items-center gap-3'>
                      <Checkbox
                        aria-label={row.group}
                        checked={values?.[row.index]?.enabled ?? false}
                        onCheckedChange={(checked) =>
                          form.setValue(
                            `rules.${row.index}.enabled`,
                            Boolean(checked),
                            { shouldDirty: true }
                          )
                        }
                      />
                      <span className='break-all'>{row.group}</span>
                    </div>
                  ),
                },
                {
                  id: 'status',
                  header: t('Local error HTTP status'),
                  cell: (row) => (
                    <FormField
                      control={form.control}
                      name={`rules.${row.index}.status_code`}
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel className='sr-only'>
                            {t('Local error HTTP status')} — {row.group}
                          </FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={400}
                              max={599}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  ),
                },
              ]}
            />
          )}
          {Object.keys(dirtyFields).length > 0 && (
            <span className='sr-only'>{t('Unsaved changes')}</span>
          )}
        </fieldset>
      </SettingsCard>
    </Form>
  )
}
