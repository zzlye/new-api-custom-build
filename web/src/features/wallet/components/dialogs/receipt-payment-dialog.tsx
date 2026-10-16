import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { QRCodeCanvas } from 'qrcode.react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestamp } from '@/lib/format'

import { getReceiptOrder, updateReceiptOrder } from '../../receipt-api'

export function ReceiptPaymentDialog(props: {
  tradeNo: string | null
  onClose: () => void
  onPaid?: () => void | Promise<void>
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [confirmClose, setConfirmClose] = useState(false)
  const query = useQuery({
    queryKey: ['receipt-order', props.tradeNo],
    queryFn: () => getReceiptOrder(props.tradeNo || ''),
    enabled: Boolean(props.tradeNo),
    refetchInterval: (state) =>
      state.state.data?.status === 'pending' ? 10000 : false,
  })
  const action = useMutation({
    mutationFn: (value: 'confirm' | 'close') =>
      updateReceiptOrder(props.tradeNo || '', value),
    onSuccess: (data) => {
      client.setQueryData(['receipt-order', props.tradeNo], data)
      setConfirmClose(false)
    },
  })
  const order = query.data
  const onPaid = props.onPaid
  useEffect(() => {
    // 账务确认成功后刷新钱包，避免显示旧余额让用户误以为没有到账。
    if (order?.status === 'success') void onPaid?.()
  }, [order?.status, onPaid])
  const expired = Boolean(
    order && order.receipt.expires_at * 1000 <= Date.now()
  )
  const payable = order?.status === 'pending' && !expired && !order.paid_time
  return (
    <>
      <Dialog
        open={Boolean(props.tradeNo)}
        onOpenChange={(open) => {
          if (!open) props.onClose()
        }}
        title={t('Alipay receipt code')}
        footer={
          <>
            {order?.status === 'pending' && !order.paid_time ? (
              <Button variant='outline' onClick={() => setConfirmClose(true)}>
                {t('Close order')}
              </Button>
            ) : null}
            <Button
              variant='outline'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('Refresh')}
            </Button>
            <Button onClick={props.onClose}>{t('Close')}</Button>
          </>
        }
      >
        {query.isPending ? <p>{t('Loading...')}</p> : null}
        {query.isError ? <p role='alert'>{t('Failed to load order')}</p> : null}
        {order ? (
          <div className='space-y-4 text-center'>
            <div className='flex items-center justify-center gap-2 text-sm break-all'>
              {order.trade_no}
              <CopyButton value={order.trade_no} />
            </div>
            <div className='text-3xl font-semibold'>
              ¥{(order.receipt.amount_cents / 100).toFixed(2)}
            </div>
            <p>
              {t('Balance credit')}:{' '}
              {formatQuotaWithCurrency(order.receipt.credit_quota)}
            </p>
            {order.status === 'success' ? (
              <p className='text-success'>{t('Payment credited')}</p>
            ) : null}
            {order.status === 'closed' ? <p>{t('Order closed')}</p> : null}
            {order.status === 'pending' && order.paid_time > 0 ? (
              <p>{t('Payment received, crediting balance')}</p>
            ) : null}
            {expired && order.status === 'pending' && !order.paid_time ? (
              <p>{t('Payment window ended. Do not pay this order.')}</p>
            ) : null}
            {order.status === 'expired' ? <p>{t('Order expired')}</p> : null}
            {payable ? (
              <>
                <p>{t('Enter the exact amount shown, including cents.')}</p>
                <p className='text-muted-foreground text-sm'>
                  {t('Expires at')}: {formatTimestamp(order.receipt.expires_at)}
                </p>
                {order.receipt.qr_code ? (
                  <div className='payment-qr-code mx-auto w-fit rounded-lg p-4'>
                    {/* 使用画布保留二维码原始像素，避免强制暗色模式重绘 SVG 黑白色块。 */}
                    <QRCodeCanvas
                      bgColor='#ffffff'
                      fgColor='#000000'
                      value={order.receipt.qr_code}
                      size={208}
                      marginSize={4}
                      role='img'
                      aria-label={t('Alipay receipt code')}
                      title={t('Alipay receipt code')}
                    />
                  </div>
                ) : (
                  <Button
                    disabled={action.isPending}
                    onClick={() => action.mutate('confirm')}
                  >
                    {t('Confirm amount and show code')}
                  </Button>
                )}
              </>
            ) : null}
          </div>
        ) : null}
      </Dialog>
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        title={t('Close order')}
        desc={t(
          'Closing this order does not refund a payment. Do not pay after closing.'
        )}
        confirmText={t('Close order')}
        destructive
        isLoading={action.isPending}
        handleConfirm={() => action.mutate('close')}
      />
    </>
  )
}
