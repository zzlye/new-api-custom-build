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
import { useQueryClient } from '@tanstack/react-query'
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
  type AppearanceConfig,
  type HomeBgType,
} from '@/lib/appearance'
import { THEME_PRESETS, type ThemePreset } from '@/lib/theme-customization'
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
  theme_preset: z.string(),
  home_bg_type: z.enum(['none', 'solid', 'image', 'video']),
  home_bg_color: z.string(),
  home_bg_media: z.string(),
})

type AppearanceFormValues = z.infer<typeof appearanceSchema>

type AppearanceSectionProps = {
  defaultValues: AppearanceConfig
}

const OPTION_KEYS: Record<keyof AppearanceFormValues, string> = {
  theme_preset: 'appearance_setting.theme_preset',
  home_bg_type: 'appearance_setting.home_bg_type',
  home_bg_color: 'appearance_setting.home_bg_color',
  home_bg_media: 'appearance_setting.home_bg_media',
}

export function AppearanceSection({ defaultValues }: AppearanceSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
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
        theme_preset: defaultValues.theme_preset,
        home_bg_type: defaultValues.home_bg_type,
        home_bg_color: defaultValues.home_bg_color,
        home_bg_media: defaultValues.home_bg_media,
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
      theme_preset: defaultValues.theme_preset,
      home_bg_type: defaultValues.home_bg_type,
      home_bg_color: defaultValues.home_bg_color,
      home_bg_media: defaultValues.home_bg_media,
    })
  }, [defaultValues, form])

  const bgType = form.watch('home_bg_type') as HomeBgType
  const mediaUrl = form.watch('home_bg_media')
  const solidColor = form.watch('home_bg_color')
  const themePreset = form.watch('theme_preset') as ThemePreset

  const refreshStatus = () => {
    queryClient.invalidateQueries({ queryKey: ['status'] })
    queryClient.invalidateQueries({ queryKey: ['system-options'] })
    try {
      window.localStorage.removeItem('status')
    } catch {
      /* empty */
    }
  }

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
      // 上传接口已写入数据库，立即刷新 status 让主页生效
      refreshStatus()
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

            {/* ── 1. 整站配色方案 ── */}
            <div className='space-y-3'>
              <div>
                <h3 className='text-sm font-semibold'>
                  {t('Site color theme')}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Applies site-wide: buttons, links, success toasts (Welcome back), warnings, charts, sidebar accents, and more.'
                  )}
                </p>
              </div>
              <FormField
                control={form.control}
                name='theme_preset'
                render={({ field }) => (
                  <FormItem>
                    <div className='grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                      {THEME_PRESETS.map((preset) => {
                        const selected = field.value === preset.value
                        return (
                          <button
                            key={preset.value}
                            type='button'
                            onClick={() => field.onChange(preset.value)}
                            className={cn(
                              'flex items-center gap-3 rounded-xl border px-3 py-3 text-left transition-colors',
                              selected
                                ? 'border-primary bg-primary/5 ring-primary/30 ring-2'
                                : 'border-border hover:bg-muted/40'
                            )}
                          >
                            <span className='flex shrink-0 gap-1'>
                              {preset.swatches.map((c) => (
                                <span
                                  key={c}
                                  className='size-5 rounded-full border shadow-sm'
                                  style={{ background: c }}
                                />
                              ))}
                            </span>
                            <span className='min-w-0'>
                              <span className='block truncate text-sm font-medium'>
                                {t(preset.name)}
                              </span>
                              <span className='text-muted-foreground block truncate text-[11px]'>
                                {preset.value}
                              </span>
                            </span>
                          </button>
                        )
                      })}
                    </div>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <div className='flex flex-wrap gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => {
                    // 临时应用当前选中预设预览
                    const body = document.body
                    if (themePreset === 'default') {
                      body.removeAttribute('data-theme-preset')
                    } else {
                      body.setAttribute('data-theme-preset', themePreset)
                    }
                    toast.success(t('Welcome back!'))
                  }}
                >
                  {t('Preview theme + success toast')}
                </Button>
              </div>
            </div>

            <div className='bg-border my-2 h-px w-full' />

            {/* ── 2. 主页背景 ── */}
            <div className='space-y-3'>
              <div>
                <h3 className='text-sm font-semibold'>
                  {t('Homepage background')}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Only affects the public homepage. Solid color, image, or short video (max 200MB).'
                  )}
                </p>
              </div>

              <SettingsFormGrid>
                <FormField
                  control={form.control}
                  name='home_bg_type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Background type')}</FormLabel>
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
                                  ? '/uploads/appearance/xxx.mp4'
                                  : '/uploads/appearance/xxx.jpg'
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
                                {uploading
                                  ? t('Uploading...')
                                  : t('Upload file')}
                              </span>
                            </Button>
                          </div>
                          {mediaUrl ? (
                            <div className='border-border bg-muted/30 mt-3 overflow-hidden rounded-lg border'>
                              {bgType === 'video' ||
                              /\.(mp4|webm|mov)(\?|$)/i.test(mediaUrl) ? (
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
              </SettingsFormGrid>
            </div>
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}
