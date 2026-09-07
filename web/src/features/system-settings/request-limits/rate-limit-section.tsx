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
import { Code2, Palette } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
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
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  RateLimitTargetTypeToggle,
  type RateLimitTargetType,
} from './rate-limit-dialog'
import { RateLimitVisualEditor } from './rate-limit-visual-editor'

const isValidJSON = (value: string | undefined) => {
  if (!value || value.trim() === '') return true
  try {
    const parsed = JSON.parse(value)
    if (typeof parsed !== 'object' || Array.isArray(parsed)) {
      return false
    }
    for (const [target, val] of Object.entries(parsed)) {
      if (!target.trim()) return false
      if (!Array.isArray(val) || val.length !== 2) return false
      if (typeof val[0] !== 'number' || typeof val[1] !== 'number') return false
      if (!Number.isInteger(val[0]) || !Number.isInteger(val[1])) return false
      if (val[0] < 0 || val[1] < 1) return false
      if (val[0] > 2147483647 || val[1] > 2147483647) return false
    }
    return true
  } catch {
    return false
  }
}

const createRateLimitSchema = (t: (key: string) => string) =>
  z.object({
    AsyncMediaConcurrency: z
      .number({ error: t('Enter an integer from 0 to 256.') })
      .int(t('Enter an integer from 0 to 256.'))
      .min(0, t('Enter an integer from 0 to 256.'))
      .max(256, t('Enter an integer from 0 to 256.')),
    ModelRequestRateLimitEnabled: z.boolean(),
    ModelRequestRateLimitDurationMinutes: z.number().min(0),
    ModelRequestRateLimitCount: z.number().min(0).max(100000000),
    ModelRequestRateLimitSuccessCount: z.number().min(1).max(100000000),
    ModelRequestRateLimitGroup: z
      .string()
      .optional()
      .refine(isValidJSON, {
        message: t('Invalid JSON format or values out of allowed range'),
      }),
    ModelRequestRateLimitModel: z
      .string()
      .optional()
      .refine(isValidJSON, {
        message: t('Invalid JSON format or values out of allowed range'),
      }),
  })

type RateLimitFormValues = z.infer<ReturnType<typeof createRateLimitSchema>>

type RateLimitSectionProps = {
  defaultValues: RateLimitFormValues
}

export function RateLimitSection({ defaultValues }: RateLimitSectionProps) {
  const { t } = useTranslation()
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const updateOption = useUpdateOption()
  const [useVisualEditor, setUseVisualEditor] = useState(true)
  const [jsonTargetType, setJsonTargetType] =
    useState<RateLimitTargetType>('group')

  const rateLimitSchema = createRateLimitSchema(t)

  const form = useForm<RateLimitFormValues>({
    resolver: zodResolver(rateLimitSchema),
    mode: 'onChange', // Enable real-time validation
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: RateLimitFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        (key !== 'AsyncMediaConcurrency' || isRoot) &&
        value !== defaultValues[key as keyof RateLimitFormValues]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value: value ?? '' })
    }
  }

  return (
    <SettingsSection title={t('Rate Limiting')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save rate limits'
          />
          <FormField
            control={form.control}
            name='ModelRequestRateLimitEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable rate limiting')}</FormLabel>
                  <FormDescription>
                    {t(
                      'This controls model request rate limiting. Web/API route throttling is configured by environment variables and may still return 429.'
                    )}
                  </FormDescription>
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

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='ModelRequestRateLimitDurationMinutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Limit period')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 0)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('minutes')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Time window for rate limiting')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ModelRequestRateLimitCount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max requests per period')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={0}
                        max={100000000}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 0)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('times')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Including failed requests, 0 = unlimited')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='ModelRequestRateLimitSuccessCount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max successful requests')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={100000000}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 1)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('times')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Only successful requests')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='AsyncMediaConcurrency'
            render={({ field }) => (
              <FormItem className='border-t pt-4'>
                <FormLabel>{t('Background media concurrency')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={256}
                    step={1}
                    className='max-w-xs'
                    {...field}
                    disabled={!isRoot}
                    onChange={(event) =>
                      field.onChange(Number(event.target.value))
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Simultaneous background media requests per service process. Extra tasks wait in the queue. Default: 4; 0 removes this extra limit. Request rate limits still apply. Only root can change this setting.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='min-w-0 space-y-4'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <Label>{t('Target-specific rate limits')}</Label>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => setUseVisualEditor(!useVisualEditor)}
              >
                {useVisualEditor ? (
                  <>
                    <Code2 className='mr-2 h-4 w-4' />
                    {t('JSON Mode')}
                  </>
                ) : (
                  <>
                    <Palette className='mr-2 h-4 w-4' />
                    {t('Visual Mode')}
                  </>
                )}
              </Button>
            </div>

            {useVisualEditor ? (
              <>
                <RateLimitVisualEditor
                  groupValue={form.watch('ModelRequestRateLimitGroup') || ''}
                  modelValue={form.watch('ModelRequestRateLimitModel') || ''}
                  onGroupChange={(value) =>
                    form.setValue('ModelRequestRateLimitGroup', value, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                  onModelChange={(value) =>
                    form.setValue('ModelRequestRateLimitModel', value, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
                {form.formState.errors.ModelRequestRateLimitGroup?.message && (
                  <p className='text-destructive text-sm'>
                    {t('Group')}:{' '}
                    {String(
                      form.formState.errors.ModelRequestRateLimitGroup.message
                    )}
                  </p>
                )}
                {form.formState.errors.ModelRequestRateLimitModel?.message && (
                  <p className='text-destructive text-sm'>
                    {t('Model')}:{' '}
                    {String(
                      form.formState.errors.ModelRequestRateLimitModel.message
                    )}
                  </p>
                )}
              </>
            ) : (
              <div className='space-y-4'>
                <RateLimitTargetTypeToggle
                  value={jsonTargetType}
                  onChange={setJsonTargetType}
                  ariaLabel={t('Type')}
                />
                <p className='text-muted-foreground text-sm'>
                  {t('Select the target type to edit its JSON configuration.')}
                </p>
                <FormField
                  control={form.control}
                  name={
                    jsonTargetType === 'group'
                      ? 'ModelRequestRateLimitGroup'
                      : 'ModelRequestRateLimitModel'
                  }
                  render={({ field }) => {
                    const isGroup = jsonTargetType === 'group'
                    const targetName = isGroup ? 'groupName' : 'modelName'
                    const example = isGroup
                      ? `{"default": [200, 100], "vip": [0, 1000]}`
                      : `{"gpt-4o": [200, 100], "claude-sonnet": [0, 1000]}`

                    return (
                      <FormItem>
                        <FormLabel>{t(isGroup ? 'Group' : 'Model')}</FormLabel>
                        <FormControl>
                          <JsonCodeEditor
                            value={field.value || ''}
                            onChange={field.onChange}
                            name={field.name}
                            onBlur={field.onBlur}
                            textareaRef={field.ref}
                            placeholder={`{\n  "${isGroup ? 'default' : 'gpt-4o'}": [200, 100]\n}`}
                            aria-invalid={Boolean(
                              form.formState.errors[field.name]
                            )}
                          />
                        </FormControl>
                        <FormDescription>
                          <span className='block space-y-1 text-xs'>
                            <span className='block font-semibold'>
                              {t('Format:')}
                            </span>
                            <span className='block'>
                              {t('JSON object:')}{' '}
                              {`{"${targetName}": [maxRequests, maxSuccess]}`}
                            </span>
                            <span className='block'>
                              {t('Example:')} {example}
                            </span>
                            <span className='block'>
                              {t(
                                'maxRequests ≥ 0, maxSuccess ≥ 1, both ≤ 2,147,483,647'
                              )}
                            </span>
                            <span className='block'>
                              {t(
                                'Target config overrides global limits and shares the same period.'
                              )}
                            </span>
                          </span>
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )
                  }}
                />
              </div>
            )}
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
