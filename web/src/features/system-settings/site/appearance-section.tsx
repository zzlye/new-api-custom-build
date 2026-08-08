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
import { Loader2, Upload } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { api } from '@/lib/api'
import {
  SUCCESS_TONES,
  type HomeBgType,
  type AppearanceConfig,
} from '@/lib/appearance'
import { cn } from '@/lib/utils'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const MAX_UPLOAD_BYTES = 200 * 1024 * 1024

const appearanceSchema = z.object({
  home_bg_type: z.enum(['none', 'solid', 'image', 'video']),
  home_bg_color: z.string(),
  home_bg_media: z.string(),
  success_tone: z.string(),
})

type AppearanceFormValues = z.infer<typeof appearanceSchema>

type AppearanceSectionProps = {
  defaultValues: AppearanceConfig
}

const OPTION_KEYS: Record<keyof AppearanceFormValues, string> = {
  home_bg_type: 'appearance_setting.home_bg_type',
  home_bg_color: 'appearance_setting.home_bg_color',
  home_bg_media: 'appearance_setting.home_bg_media',
  success_tone: 'appearance_setting.success_tone',
}

export function AppearanceSection({ defaultValues }: AppearanceSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)

  const { form, handleSubmit, isDirty, isSubmitting, handleReset } =
    useSettingsForm<AppearanceFormValues>({
      resolver: zodResolver(appearanceSchema) as Resolver<
        AppearanceFormValues,
        unknown,
        AppearanceFormValues
      >,
      defaultValues: {
        home_bg_type: defaultValues.home_bg_type,
        home_bg_color: defaultValues.home_bg_color,
        home_bg_media: defaultValues.home_bg_media,
        success_tone: defaultValues.success_tone,
      },
      onSubmit: async (_data, changedFields) => {
        for (const [field, value] of Object.entries(changedFields)) {
          const key = OPTION_KEYS[field as keyof AppearanceFormValues]
          if (!key) continue
          await updateOption.mutateAsync({
            key,
            value: String(value ?? ''),
          })
        }
      },
    })

  useEffect(() => {
    form.reset({
      home_bg_type: defaultValues.home_bg_type,
      home_bg_color: defaultValues.home_bg_color,
      home_bg_media: defaultValues.home_bg_media,
      success_tone: defaultValues.success_tone,
    })
  }, [defaultValues, form])

  const bgType = form.watch('home_bg_type') as HomeBgType
  const mediaUrl = form.watch('home_bg_media')
  const solidColor = form.watch('home_bg_color')
  const successTone = form.watch('success_tone')

  const handleUpload = async (file: File) => {
    if (file.size > MAX_UPLOAD_BYTES) {
      toast.error(t('File must be 200MB or smaller'))
      return
    }
    const isVideo = file.type.startsWith('video/')
    const isImage = file.type.startsWith('image/')
    if (!isVideo && !isImage) {
      toast.error(t('Only images or short videos are allowed'))
      return
    }

    setUploading(true)
    try {
      const body = new FormData()
      body.append('file', file)
      const res = await api.post<{
        success: boolean
        message?: string
        data?: { url: string; type: string }
      }>('/api/option/appearance/upload', body, {
        headers: { 'Content-Type': 'multipart/form-data' },
        timeout: 10 * 60 * 1000,
      })
      if (!res.data?.success || !res.data.data?.url) {
        throw new Error(res.data?.message || t('Upload failed'))
      }
      const nextType = (res.data.data.type === 'video' ? 'video' : 'image') as
        | 'image'
        | 'video'
      form.setValue('home_bg_type', nextType, { shouldDirty: true })
      form.setValue('home_bg_media', res.data.data.url, { shouldDirty: true })
      toast.success(t('Upload successful'))
    } catch (error: unknown) {
      const message =
        error instanceof Error ? error.message : t('Upload failed')
      toast.error(message)
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  return (
    <>
      <FormNavigationGuard when={isDirty} />
      <SettingsSection title={t('Appearance')}>
        <Form {...form}>
          <SettingsForm onSubmit={handleSubmit}>
            <SettingsPageFormActions
              onSave={handleSubmit}
              onReset={handleReset}
              isSaving={isSubmitting || updateOption.isPending || uploading}
              isResetDisabled={!isDirty}
            />
            <FormDirtyIndicator isDirty={isDirty} />

            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='home_bg_type'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Homepage background type')}</FormLabel>
                    <FormControl>
                      <Select
                        items={[
                          { value: 'none', label: t('None (default)') },
                          { value: 'solid', label: t('Solid color') },
                          { value: 'image', label: t('Image') },
                          { value: 'video', label: t('Short video') },
                        ]}
                        value={field.value}
                        onValueChange={field.onChange}
                      >
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='none'>
                              {t('None (default)')}
                            </SelectItem>
                            <SelectItem value='solid'>
                              {t('Solid color')}
                            </SelectItem>
                            <SelectItem value='image'>{t('Image')}</SelectItem>
                            <SelectItem value='video'>
                              {t('Short video')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Only root can change appearance. Visitors will see the homepage background.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {bgType === 'solid' ? (
                <FormField
                  control={form.control}
                  name='home_bg_color'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Solid background color')}</FormLabel>
                      <div className='flex items-center gap-3'>
                        <input
                          type='color'
                          className='border-input h-9 w-12 cursor-pointer rounded border bg-transparent p-1'
                          value={
                            /^#[0-9A-Fa-f]{6}$/.test(field.value)
                              ? field.value
                              : '#0f172a'
                          }
                          onChange={(e) => field.onChange(e.target.value)}
                        />
                        <FormControl>
                          <Input
                            placeholder='#0f172a'
                            {...field}
                            className='font-mono'
                          />
                        </FormControl>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ) : null}

              {bgType === 'image' || bgType === 'video' ? (
                <SettingsFormGridItem span='full'>
                  <FormField
                    control={form.control}
                    name='home_bg_media'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {bgType === 'video'
                            ? t('Background video')
                            : t('Background image')}
                        </FormLabel>
                        <FormControl>
                          <Input
                            placeholder={
                              bgType === 'video'
                                ? 'https://.../bg.mp4 or /uploads/appearance/...'
                                : 'https://.../bg.jpg or /uploads/appearance/...'
                            }
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Paste a URL or upload a local file (max 200MB).'
                          )}
                        </FormDescription>
                        <div className='flex flex-wrap items-center gap-2 pt-1'>
                          <input
                            ref={fileInputRef}
                            type='file'
                            className='hidden'
                            accept={
                              bgType === 'video'
                                ? 'video/mp4,video/webm,video/quicktime'
                                : 'image/jpeg,image/png,image/webp,image/gif'
                            }
                            onChange={(e) => {
                              const file = e.target.files?.[0]
                              if (file) void handleUpload(file)
                            }}
                          />
                          <Button
                            type='button'
                            variant='outline'
                            disabled={uploading}
                            onClick={() => fileInputRef.current?.click()}
                          >
                            {uploading ? (
                              <Loader2 className='size-4 animate-spin' />
                            ) : (
                              <Upload className='size-4' />
                            )}
                            <span className='ml-2'>
                              {uploading ? t('Uploading...') : t('Upload file')}
                            </span>
                          </Button>
                        </div>
                        {mediaUrl ? (
                          <div className='border-border bg-muted/30 mt-3 overflow-hidden rounded-lg border'>
                            {bgType === 'video' ? (
                              <video
                                src={mediaUrl}
                                className='max-h-48 w-full object-cover'
                                muted
                                loop
                                autoPlay
                                playsInline
                              />
                            ) : (
                              <img
                                src={mediaUrl}
                                alt={t('Background preview')}
                                className='max-h-48 w-full object-cover'
                              />
                            )}
                          </div>
                        ) : null}
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SettingsFormGridItem>
              ) : null}

              {bgType === 'solid' ? (
                <SettingsFormGridItem span='full'>
                  <div
                    className='border-border h-24 rounded-lg border'
                    style={{ background: solidColor || '#0f172a' }}
                    aria-hidden
                  />
                </SettingsFormGridItem>
              ) : null}

              <SettingsFormGridItem span='full'>
                <FormField
                  control={form.control}
                  name='success_tone'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Success color (e.g. Welcome back)')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Used for success toasts such as “Welcome back!” after login.'
                        )}
                      </FormDescription>
                      <div className='grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4'>
                        {SUCCESS_TONES.map((tone) => {
                          const selected = field.value === tone.value
                          return (
                            <button
                              key={tone.value}
                              type='button'
                              onClick={() => field.onChange(tone.value)}
                              className={cn(
                                'flex items-center gap-2 rounded-lg border px-3 py-2 text-left text-sm transition-colors',
                                selected
                                  ? 'border-primary bg-primary/5 ring-primary/30 ring-2'
                                  : 'border-border hover:bg-muted/50'
                              )}
                            >
                              <span
                                className='size-4 shrink-0 rounded-full border'
                                style={{ background: tone.color }}
                              />
                              <span className='truncate'>{t(tone.name)}</span>
                            </button>
                          )
                        })}
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SettingsFormGridItem>

              {/* 成功色预览 */}
              <SettingsFormGridItem span='full'>
                <div className='flex flex-wrap items-center gap-3'>
                  <Button
                    type='button'
                    variant='outline'
                    onClick={() => {
                      const tone =
                        SUCCESS_TONES.find((x) => x.value === successTone) ||
                        SUCCESS_TONES[0]
                      // 临时覆盖 --success 做预览
                      document.documentElement.style.setProperty(
                        '--success',
                        tone.color
                      )
                      toast.success(t('Welcome back!'))
                    }}
                  >
                    {t('Preview success toast')}
                  </Button>
                </div>
              </SettingsFormGridItem>
            </SettingsFormGrid>
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}
