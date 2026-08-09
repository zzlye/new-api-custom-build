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
import type { Resolver, UseFormReturn } from 'react-hook-form'
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
import { Slider } from '@/components/ui/slider'
import { api } from '@/lib/api'
import type { AppearanceConfig, BgType } from '@/lib/appearance'
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

const bgTypeEnum = z.enum(['none', 'solid', 'image', 'video'])

const appearanceSchema = z.object({
  theme_preset: z.string(),
  home_bg_type: bgTypeEnum,
  home_bg_color: z.string(),
  home_bg_media: z.string(),
  home_bg_overlay_opacity: z.number().min(0).max(1),
  login_bg_type: bgTypeEnum,
  login_bg_color: z.string(),
  login_bg_media: z.string(),
  login_bg_overlay_opacity: z.number().min(0).max(1),
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
  home_bg_overlay_opacity: 'appearance_setting.home_bg_overlay_opacity',
  login_bg_type: 'appearance_setting.login_bg_type',
  login_bg_color: 'appearance_setting.login_bg_color',
  login_bg_media: 'appearance_setting.login_bg_media',
  login_bg_overlay_opacity: 'appearance_setting.login_bg_overlay_opacity',
}

type BgFieldPrefix = 'home' | 'login'

/** 主页 / 登录页共用的背景字段块 */
function BackgroundFields({
  form,
  prefix,
  uploading,
  onUpload,
}: {
  form: UseFormReturn<AppearanceFormValues>
  prefix: BgFieldPrefix
  uploading: boolean
  onUpload: (file: File, target: BgFieldPrefix) => void
}) {
  const { t } = useTranslation()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const typeKey = `${prefix}_bg_type` as const
  const colorKey = `${prefix}_bg_color` as const
  const mediaKey = `${prefix}_bg_media` as const
  const opacityKey = `${prefix}_bg_overlay_opacity` as const
  const bgType = form.watch(typeKey) as BgType
  const mediaUrl = form.watch(mediaKey)
  const solidColor = form.watch(colorKey)
  const overlayOpacity = form.watch(opacityKey)

  return (
    <SettingsFormGrid>
      <FormField
        control={form.control}
        name={typeKey}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Background type')}</FormLabel>
            <FormControl>
              <Select
                items={[
                  { value: 'none', label: t('None (default)') },
                  { value: 'solid', label: t('Solid color') },
                  { value: 'image', label: t('Image') },
                  { value: 'video', label: t('Video') },
                ]}
                value={field.value}
                onValueChange={field.onChange}
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectItem value='none'>{t('None (default)')}</SelectItem>
                    <SelectItem value='solid'>{t('Solid color')}</SelectItem>
                    <SelectItem value='image'>{t('Image')}</SelectItem>
                    <SelectItem value='video'>{t('Video')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {bgType === 'image' || bgType === 'video' ? (
        <SettingsFormGridItem span='full'>
          <FormField
            control={form.control}
            name={opacityKey}
            render={({ field }) => {
              const percent = Math.round(Number(field.value) * 100)
              return (
                <FormItem>
                  <div className='flex items-center justify-between gap-3'>
                    <FormLabel>{t('Background overlay opacity')}</FormLabel>
                    <span className='text-muted-foreground text-sm tabular-nums'>
                      {percent}%
                    </span>
                  </div>
                  <FormControl>
                    <Slider
                      aria-label={t('Background overlay opacity')}
                      max={1}
                      min={0}
                      step={0.01}
                      value={[Number(overlayOpacity)]}
                      onValueChange={(nextValue) => {
                        const value = Array.isArray(nextValue)
                          ? nextValue[0]
                          : nextValue
                        field.onChange(Number(value))
                      }}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('0% is clear; 100% is fully dark.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )
            }}
          />
        </SettingsFormGridItem>
      ) : null}

      {bgType === 'solid' ? (
        <FormField
          control={form.control}
          name={colorKey}
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
            name={mediaKey}
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
                  {t('Paste a URL or upload a local file (max 200MB).')}
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
                      if (file) onUpload(file, prefix)
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
  )
}

export function AppearanceSection({ defaultValues }: AppearanceSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const [uploading, setUploading] = useState(false)

  const formDefaults: AppearanceFormValues = {
    theme_preset: defaultValues.theme_preset,
    home_bg_type: defaultValues.home_bg_type,
    home_bg_color: defaultValues.home_bg_color,
    home_bg_media: defaultValues.home_bg_media,
    home_bg_overlay_opacity: defaultValues.home_bg_overlay_opacity,
    login_bg_type: defaultValues.login_bg_type,
    login_bg_color: defaultValues.login_bg_color,
    login_bg_media: defaultValues.login_bg_media,
    login_bg_overlay_opacity: defaultValues.login_bg_overlay_opacity,
  }

  const { form, handleSubmit, isDirty, isSubmitting, handleReset } =
    useSettingsForm<AppearanceFormValues>({
      resolver: zodResolver(appearanceSchema) as Resolver<
        AppearanceFormValues,
        unknown,
        AppearanceFormValues
      >,
      defaultValues: formDefaults,
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
    form.reset(formDefaults)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 仅随 defaultValues 同步
  }, [defaultValues, form])

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

  const handleUpload = async (file: File, target: BgFieldPrefix) => {
    if (file.size > MAX_UPLOAD_BYTES) {
      toast.error(t('File must be 200MB or smaller'))
      return
    }
    const isVideo = file.type.startsWith('video/')
    const isImage = file.type.startsWith('image/')
    if (!isVideo && !isImage) {
      toast.error(t('Only images or videos are allowed'))
      return
    }

    setUploading(true)
    try {
      const body = new FormData()
      body.append('file', file)
      body.append('target', target)
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
      form.setValue(`${target}_bg_type`, nextType, { shouldDirty: true })
      form.setValue(`${target}_bg_media`, res.data.data.url, {
        shouldDirty: true,
      })
      refreshStatus()
      toast.success(t('Upload successful'))
    } catch (error: unknown) {
      const message =
        error instanceof Error ? error.message : t('Upload failed')
      toast.error(message)
    } finally {
      setUploading(false)
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

            {/* 1. 整站配色 */}
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
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => {
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

            <div className='bg-border my-2 h-px w-full' />

            {/* 2. 主页背景 */}
            <div className='space-y-3'>
              <div>
                <h3 className='text-sm font-semibold'>
                  {t('Homepage background')}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Only affects the public homepage. Solid color, image, or video (max 200MB).'
                  )}
                </p>
              </div>
              <BackgroundFields
                form={form}
                prefix='home'
                uploading={uploading}
                onUpload={handleUpload}
              />
            </div>

            <div className='bg-border my-2 h-px w-full' />

            {/* 3. 登录页背景 */}
            <div className='space-y-3'>
              <div>
                <h3 className='text-sm font-semibold'>
                  {t('Login page background')}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Applies to sign-in, sign-up, password reset and other auth pages.'
                  )}
                </p>
              </div>
              <BackgroundFields
                form={form}
                prefix='login'
                uploading={uploading}
                onUpload={handleUpload}
              />
            </div>
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}
