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

const policySchema = z.object({
  enabled: z.boolean(),
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
type Policy = z.infer<typeof policySchema>

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

function LocalRequestValidationEditor(props: { defaults: Policy }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [search, setSearch] = useState('')
  const form = useForm<Policy>({
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
                        checked={values[row.index]?.enabled ?? false}
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
