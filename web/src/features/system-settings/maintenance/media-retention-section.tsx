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
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

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
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  mediaRetentionSchema,
  type MediaRetentionValues,
} from '../lib/media-retention'

// 保存时长仅向根用户展示，其他用户的任务查询不包含设置入口。
export function MediaRetentionSection(props: { defaultHours: number }) {
  const { t } = useTranslation()
  const role = useAuthStore((state) => state.auth.user?.role)
  const updateOption = useUpdateOption()
  const defaults = { hours: props.defaultHours }
  const form = useForm<MediaRetentionValues>({
    resolver: zodResolver(mediaRetentionSchema),
    defaultValues: defaults,
  })
  useResetForm(form, defaults)
  const onSubmit = async (values: MediaRetentionValues) => {
    await updateOption.mutateAsync({
      key: 'AsyncMediaRetentionHours',
      value: values.hours,
    })
  }
  if (role !== ROLE.SUPER_ADMIN) return null
  return (
    <SettingsSection title={t('Generated media retention')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save media retention settings'
          />
          <FormField
            control={form.control}
            name='hours'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Retention period (hours)')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    type='number'
                    min={1}
                    max={168}
                    step={1}
                    onChange={(event) =>
                      field.onChange(event.target.valueAsNumber)
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Default: 2 hours after completion. Enter 1 to 168 whole hours. Changes apply to files that have not been removed. Only root users can change this setting or delete task logs.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
