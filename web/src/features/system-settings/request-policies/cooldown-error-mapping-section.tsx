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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { updateSystemOption } from '../api'
import { SettingsCard } from '../components/settings-card'
import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'

// 按行录入用户名；去除编辑时的空行和首尾空白，保留用户名大小写。
const schema = z.object({
  enabled: z.boolean(),
  usernames: z
    .array(
      z
        .string()
        .min(1)
        .refine(
          (name) =>
            [...name].length <= 64 &&
            !/[\r\n]/.test(name) &&
            !name.includes(String.fromCharCode(0)),
          {
            message:
              'Each username must be at most 64 characters without line breaks.',
          }
        )
    )
    .max(1000, { message: 'Enter up to 1000 usernames.' })
    .refine((names) => new Set(names).size === names.length, {
      message: 'Usernames must not be duplicated.',
    }),
})
type MappingPolicy = z.infer<typeof schema>
function parseUsernameList(text: string): string[] {
  return text
    .split('\n')
    .map((name) => name.trim())
    .filter(Boolean)
}
const formSchema = schema.extend({
  usernames: z.string().superRefine((text, context) => {
    const result = schema.shape.usernames.safeParse(parseUsernameList(text))
    if (!result.success) {
      context.addIssue({
        code: 'custom',
        message: result.error.issues[0].message,
      })
    }
  }),
})
type MappingInput = z.input<typeof formSchema>

export function CooldownErrorMappingSection(props: {
  value: string
  active: boolean
}) {
  const { t } = useTranslation()
  const defaults = useMemo(() => {
    try {
      return schema.parse(JSON.parse(props.value))
    } catch {
      return null
    }
  }, [props.value])
  if (!defaults) return <ErrorState title={t('Failed to load settings')} />
  return (
    <CooldownErrorMappingEditor defaults={defaults} active={props.active} />
  )
}

function CooldownErrorMappingEditor(props: {
  defaults: MappingPolicy
  active: boolean
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<MappingInput>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      ...props.defaults,
      usernames: props.defaults.usernames.join('\n'),
    },
  })
  useEffect(() => {
    form.reset(
      { ...props.defaults, usernames: props.defaults.usernames.join('\n') },
      { keepDirtyValues: true }
    )
  }, [props.defaults, form])
  // 订阅脏字段，使后台重新读取设置时保留另一分页未保存的输入。
  const { dirtyFields } = form.formState
  const save = useMutation({
    mutationFn: async (policy: MappingPolicy) => {
      requireServerSuccess(
        await updateSystemOption({
          key: 'CooldownErrorMappingPolicy',
          value: JSON.stringify(policy),
        })
      )
      return policy
    },
    onSuccess: (policy) => {
      form.reset({ ...policy, usernames: policy.usernames.join('\n') })
      toast.success(t('Settings updated successfully'))
      void client.invalidateQueries({ queryKey: ['system-options'] })
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <Form {...form}>
      <SettingsCard title={t('Cooldown error mapping')}>
        {props.active && (
          <SettingsPageFormActions
            onSave={form.handleSubmit((policy) =>
              save.mutate({
                ...policy,
                usernames: parseUsernameList(policy.usernames),
              })
            )}
            isSaving={save.isPending}
          />
        )}
        <fieldset disabled={save.isPending} className='min-w-0 space-y-4'>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable cooldown error mapping')}</FormLabel>
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
          <FormField
            control={form.control}
            name='usernames'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Mapped usernames')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={8}
                    placeholder={t('One username per line')}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='bg-muted/30 rounded-lg border px-3 py-2 text-sm'>
            <span className='text-muted-foreground'>
              {t('Response error')}:{' '}
            </span>
            <code>429 · RESOURCE_EXHAUSTED · resource exhaust</code>
          </div>
          {Object.keys(dirtyFields).length > 0 && (
            <span className='sr-only'>{t('Unsaved changes')}</span>
          )}
        </fieldset>
      </SettingsCard>
    </Form>
  )
}
