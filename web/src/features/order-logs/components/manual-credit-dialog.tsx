import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
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
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  manualCreditSchema,
  type ManualCreditValues,
} from '../lib/manual-credit-schema'

type Props = {
  order: {
    trade_no: string
    user_id: number
    username?: string
    money: number
    credit_quota: number
    provider_trade_no: string
    actual_money: number
  }
  onClose: () => void
}

export function ManualCreditDialog(props: Props) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [confirmation, setConfirmation] = useState<ManualCreditValues | null>(
    null
  )
  const form = useForm<ManualCreditValues>({
    resolver: zodResolver(manualCreditSchema),
    defaultValues: {
      actual_money:
        props.order.actual_money > 0 ? props.order.actual_money.toFixed(2) : '',
      provider_trade_no: props.order.provider_trade_no || '',
      reason: '',
    },
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: async (values: ManualCreditValues) => {
      // 仅发送核对资料；到账额度始终由服务端读取原订单，不能由表单指定。
      const response = await api.post(
        `/api/user/order-logs/${encodeURIComponent(props.order.trade_no)}/manual-credit`,
        values
      )
      requireServerSuccess(response.data)
    },
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['order-logs'] }),
        client.invalidateQueries({ queryKey: ['order-log-detail'] }),
      ])
      props.onClose()
    },
  })
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open && !mutation.isPending && !confirmation) props.onClose()
        }}
        title={t('Manual order credit')}
        footer={
          <Button
            type='submit'
            form='manual-credit-form'
            disabled={mutation.isPending}
          >
            {t('Review credit')}
          </Button>
        }
      >
        <dl className='mb-4 grid grid-cols-2 gap-2 text-sm'>
          <dt>{t('User')}</dt>
          <dd>
            {props.order.username || '-'} · ID: {props.order.user_id}
          </dd>
          <dt>{t('Order number')}</dt>
          <dd className='break-all'>{props.order.trade_no}</dd>
          <dt>{t('Amount due')}</dt>
          <dd>¥{props.order.money.toFixed(2)}</dd>
          <dt>{t('Balance credit')}</dt>
          <dd>{formatQuotaWithCurrency(props.order.credit_quota)}</dd>
        </dl>
        <Form {...form}>
          <form
            id='manual-credit-form'
            className='space-y-4'
            onSubmit={form.handleSubmit((values) => {
              mutation.reset()
              setConfirmation(values)
            })}
          >
            <FormField
              control={form.control}
              name='actual_money'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Amount received')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      inputMode='decimal'
                      maxLength={9}
                      disabled={mutation.isPending}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='provider_trade_no'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Alipay transaction number')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      inputMode='numeric'
                      maxLength={64}
                      disabled={mutation.isPending}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='reason'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Reason for manual credit')}</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      maxLength={500}
                      disabled={mutation.isPending}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </form>
        </Form>
      </Dialog>
      <ConfirmDialog
        open={Boolean(confirmation)}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setConfirmation(null)
        }}
        title={t('Confirm manual credit')}
        desc={t(
          'Credit the original order amount of {{credit}} after confirming receipt.',
          { credit: formatQuotaWithCurrency(props.order.credit_quota) }
        )}
        confirmText={t('Confirm credit')}
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (confirmation && !mutation.isPending) mutation.mutate(confirmation)
        }}
      >
        <div className='space-y-2 text-sm break-all'>
          <p>
            {t('Order number')}: {props.order.trade_no} · ID:{' '}
            {props.order.user_id}
          </p>
          <p>
            {t('Amount received')}: ¥{confirmation?.actual_money}
          </p>
          <p>
            {t('Alipay transaction number')}: {confirmation?.provider_trade_no}
          </p>
          <p>
            {t('Reason for manual credit')}: {confirmation?.reason}
          </p>
          {mutation.isError ? (
            <p role='alert' className='text-destructive'>
              {mutation.error.message}
            </p>
          ) : null}
        </div>
      </ConfirmDialog>
    </>
  )
}
