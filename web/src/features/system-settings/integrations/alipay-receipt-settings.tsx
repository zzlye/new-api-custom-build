import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
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

export function AlipayReceiptSettings() {
  const { t } = useTranslation()
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
    resolver: zodResolver(schema),
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
          isSaveDisabled={query.isPending || query.isError}
        />
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
              'qr_code',
              'daily_limit',
              'private_key',
              'public_key',
            ] as const
          ).map((name) => {
            const labels = {
              account_id: t('Alipay account UID'),
              app_id: 'APPID',
              qr_code: t('Receipt code URL'),
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
          disabled={query.isPending || query.isError || mutation.isPending}
          onClick={() => void save()}
        >
          {t('Save')}
        </Button>
      </div>
    </Form>
  )
}
