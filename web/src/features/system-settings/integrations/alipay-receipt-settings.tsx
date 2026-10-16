import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { formatTimestamp } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { SettingsPageFormActions } from '../components/settings-page-context'

const schema = z.object({
  enabled: z.boolean(),
  account_id: z.string().max(32),
  app_id: z.string().max(32),
  qr_code: z.string().max(2048),
  private_key: z.string().max(8192),
  public_key: z.string().max(8192),
  daily_limit: z.number().int().min(1).max(1000),
})
type Config = z.infer<typeof schema>
interface Settings {
  config: Config
  private_key_configured: boolean
  last_scan_at: number
  last_error: string
}

// 只接受支付宝收款码内容，不能把图片地址或其他站点二维码作为支付入口。
const receiptCodeSchema = z
  .string()
  .max(2048)
  .regex(/^https:\/\/qr\.alipay\.com\/[^\s]+$/)

export function AlipayReceiptSettings() {
  const { t } = useTranslation()
  const imageInput = useRef<HTMLInputElement>(null)
  const imageRequest = useRef(0)
  const [readingImage, setReadingImage] = useState(false)
  const [imageError, setImageError] = useState('')
  useEffect(
    () => () => {
      imageRequest.current += 1
    },
    []
  )
  const query = useQuery({
    queryKey: ['alipay-receipt-settings'],
    refetchOnWindowFocus: false,
    queryFn: async (): Promise<Settings> => {
      const response = await api.get('/api/option/alipay-receipt')
      requireServerSuccess(response.data)
      return response.data.data
    },
  })
  const form = useForm<Config>({
    resolver: zodResolver(
      schema.extend({
        qr_code: z
          .string()
          .trim()
          .refine(
            (value) =>
              value === '' || receiptCodeSchema.safeParse(value).success,
            { message: t('Please use an Alipay receipt code URL') }
          ),
      })
    ),
    values: query.data?.config,
    defaultValues: {
      enabled: false,
      account_id: '',
      app_id: '',
      qr_code: '',
      private_key: '',
      public_key: '',
      daily_limit: 10,
    },
  })
  const qrCode = form.watch('qr_code')
  const validQRCode = receiptCodeSchema.safeParse(qrCode.trim()).success
  async function handleReceiptImage(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    const request = ++imageRequest.current
    setImageError('')
    if (
      !['image/png', 'image/jpeg', 'image/webp'].includes(file.type) ||
      file.size > 10 * 1024 * 1024
    ) {
      setImageError(t('Choose a PNG, JPG or WebP image up to 10 MB'))
      return
    }
    setReadingImage(true)
    let url: string | undefined
    try {
      // 图片仅在浏览器内识别，服务端仍保存原有收款码地址，不增加图片存储。
      const image = new Image()
      url = URL.createObjectURL(file)
      image.src = url
      await image.decode()
      if (request !== imageRequest.current) return
      if (
        !image.naturalWidth ||
        !image.naturalHeight ||
        image.naturalWidth * image.naturalHeight > 32_000_000
      ) {
        throw new Error('invalid-image-size')
      }
      const scale = Math.min(
        1,
        2048 / Math.max(image.naturalWidth, image.naturalHeight)
      )
      const canvas = document.createElement('canvas')
      canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
      canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
      const context = canvas.getContext('2d')
      if (!context) throw new Error('canvas-unavailable')
      context.drawImage(image, 0, 0, canvas.width, canvas.height)
      const pixels = context.getImageData(0, 0, canvas.width, canvas.height)
      const { default: jsQR } = await import('jsqr')
      if (request !== imageRequest.current) return
      const result = jsQR(pixels.data, pixels.width, pixels.height)
      if (!result) {
        setImageError(t('No QR code found. Choose a clearer image.'))
        return
      }
      const value = result.data.trim()
      if (!receiptCodeSchema.safeParse(value).success) {
        setImageError(t('Please choose an Alipay receipt code image'))
        return
      }
      form.setValue('qr_code', value, {
        shouldDirty: true,
        shouldValidate: true,
      })
    } catch {
      if (request === imageRequest.current) {
        setImageError(t('Unable to read this image'))
      }
    } finally {
      if (url) URL.revokeObjectURL(url)
      if (request === imageRequest.current) setReadingImage(false)
    }
  }
  const mutation = useMutation({
    mutationFn: async (config: Config) => {
      const response = await api.post('/api/option/alipay-receipt', config)
      requireServerSuccess(response.data)
    },
    onSuccess: async () => {
      form.setValue('private_key', '')
      await query.refetch()
      toast.success(t('Saved successfully'))
    },
  })
  const save = form.handleSubmit((config) => mutation.mutate(config))
  return (
    <Form {...form}>
      <div className='space-y-6'>
        <SettingsPageFormActions
          onSave={save}
          isSaving={mutation.isPending}
          isSaveDisabled={query.isPending || query.isError || readingImage}
        />
        <div className='space-y-3 rounded-lg border p-4'>
          <Label htmlFor='alipay-receipt-image'>
            {t('Alipay receipt code')}
          </Label>
          <div className='flex flex-wrap items-start gap-4'>
            {validQRCode ? (
              <div className='rounded-md bg-white p-3'>
                <QRCodeSVG
                  value={qrCode.trim()}
                  size={160}
                  marginSize={2}
                  role='img'
                  aria-label={t('Receipt code preview')}
                />
              </div>
            ) : null}
            <div className='space-y-3'>
              <input
                id='alipay-receipt-image'
                ref={imageInput}
                type='file'
                accept='image/png,image/jpeg,image/webp'
                className='sr-only'
                disabled={
                  readingImage ||
                  query.isPending ||
                  query.isError ||
                  mutation.isPending
                }
                onChange={(event) => void handleReceiptImage(event)}
              />
              <Button
                type='button'
                variant='outline'
                disabled={
                  readingImage ||
                  query.isPending ||
                  query.isError ||
                  mutation.isPending
                }
                onClick={() => imageInput.current?.click()}
              >
                {readingImage
                  ? t('Reading receipt code...')
                  : t('Choose receipt code image')}
              </Button>
              {imageError ? (
                <p role='alert' className='text-destructive text-sm'>
                  {imageError}
                </p>
              ) : null}
            </div>
          </div>
          <FormField
            control={form.control}
            name='qr_code'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Receipt code URL')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='https://qr.alipay.com/…'
                    disabled={readingImage || mutation.isPending}
                    onChange={(event) => {
                      setImageError('')
                      field.onChange(event)
                    }}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between'>
              <FormLabel>{t('Enable Alipay receipt code')}</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <div className='grid gap-6 md:grid-cols-2'>
          {(
            [
              'account_id',
              'app_id',
              'daily_limit',
              'private_key',
              'public_key',
            ] as const
          ).map((name) => {
            const labels = {
              account_id: t('Alipay account UID'),
              app_id: 'APPID',
              daily_limit: t('Daily orders per user'),
              private_key: t('Application private key'),
              public_key: t('Alipay public key'),
            }
            return (
              <FormField
                key={name}
                control={form.control}
                name={name}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{labels[name]}</FormLabel>
                    <FormControl>
                      {name === 'private_key' || name === 'public_key' ? (
                        <Textarea
                          {...field}
                          rows={4}
                          autoComplete='off'
                          placeholder={
                            name === 'private_key' &&
                            query.data?.private_key_configured
                              ? t('Configured. Leave blank to keep.')
                              : ''
                          }
                        />
                      ) : (
                        <Input
                          {...field}
                          type={name === 'daily_limit' ? 'number' : 'text'}
                          min={name === 'daily_limit' ? 1 : undefined}
                          max={name === 'daily_limit' ? 1000 : undefined}
                          onChange={(event) =>
                            field.onChange(
                              name === 'daily_limit'
                                ? Number(event.target.value)
                                : event.target.value
                            )
                          }
                        />
                      )}
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )
          })}
        </div>
        {query.data?.last_scan_at ? (
          <p className='text-muted-foreground text-sm'>
            {t('Last reconciliation')}:{' '}
            {formatTimestamp(query.data.last_scan_at)}
          </p>
        ) : null}
        {query.data?.last_error ? (
          <p role='alert' className='text-destructive'>
            {query.data.last_error}
          </p>
        ) : null}
        <Button
          type='button'
          disabled={
            query.isPending ||
            query.isError ||
            mutation.isPending ||
            readingImage
          }
          onClick={() => void save()}
        >
          {t('Save')}
        </Button>
      </div>
    </Form>
  )
}
