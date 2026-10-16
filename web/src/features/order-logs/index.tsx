import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { SectionPageLayout } from '@/components/layout'
import { PageFooterPortal } from '@/components/layout/components/page-footer'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber, formatTimestamp } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

interface Order {
  id: number
  user_id: number
  username: string
  trade_no: string
  provider_trade_no: string
  payment_provider: string
  payment_method: string
  amount: number
  credit_quota: number
  money: number
  actual_money: number
  create_time: number
  paid_time: number
  complete_time: number
  closed_time: number
  create_ip: string
  status: string
  last_error: string
}
interface OrderDetail extends Order {
  events: {
    id: number
    created_at: number
    actor_id: number
    message: string
  }[]
}
type OrderAction = { tradeNo: string; type: 'retry' | 'close' }
async function orderRequest<T>(
  path: string,
  params?: Record<string, unknown>
): Promise<T> {
  const response = await api.get(`/api/user/order-logs${path}`, { params })
  requireServerSuccess(response.data)
  return response.data.data
}

export function OrderLogs() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const client = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('all')
  const [provider, setProvider] = useState('all')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<string | null>(null)
  const [action, setAction] = useState<OrderAction | null>(null)
  const list = useQuery({
    queryKey: ['order-logs', search, status, provider, page],
    queryFn: () =>
      orderRequest<{ items: Order[]; total: number }>('', {
        keyword: search,
        status: status === 'all' ? '' : status,
        provider: provider === 'all' ? '' : provider,
        page,
      }),
  })
  const detail = useQuery({
    queryKey: ['order-log-detail', selected],
    queryFn: () =>
      orderRequest<OrderDetail>(`/${encodeURIComponent(selected || '')}`),
    enabled: Boolean(selected),
  })
  const mutation = useMutation({
    mutationFn: async (target: OrderAction) => {
      // 操作绑定确认时的订单，不依赖详情弹窗的选择状态，避免列表操作串单。
      const response = await api.post(
        `/api/user/order-logs/${encodeURIComponent(target.tradeNo)}/${target.type}`
      )
      requireServerSuccess(response.data)
    },
    onSuccess: async () => {
      setAction(null)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['order-logs'] }),
        client.invalidateQueries({ queryKey: ['order-log-detail'] }),
      ])
    },
  })
  const statuses: Record<string, string> = {
    pending: t('Pending payment'),
    success: t('Payment credited'),
    closed: t('Order closed'),
    expired: t('Order expired'),
    failed: t('Failed'),
  }
  const providers: Record<string, string> = {
    alipay_receipt: t('Alipay receipt code'),
    epay: 'Epay',
    stripe: 'Stripe',
    creem: 'Creem',
    waffo: 'Waffo',
    waffo_pancake: 'Waffo Pancake',
  }
  const statusItems = { all: t('All statuses'), ...statuses }
  const providerItems = { all: t('All payment methods'), ...providers }
  function renderPaymentActions(order: Order) {
    if (!isRoot || order.status !== 'pending') return null
    return (
      <>
        {order.payment_provider === 'alipay_receipt' ? (
          <Button
            size='sm'
            variant='outline'
            disabled={mutation.isPending}
            onClick={() =>
              setAction({ tradeNo: order.trade_no, type: 'retry' })
            }
          >
            {t('Retry callback')}
          </Button>
        ) : null}
        <Button
          size='sm'
          variant='destructive'
          disabled={mutation.isPending || order.paid_time > 0}
          onClick={() => setAction({ tradeNo: order.trade_no, type: 'close' })}
        >
          {t('Close order')}
        </Button>
      </>
    )
  }
  const columns: StaticDataTableColumn<Order>[] = [
    {
      id: 'user',
      header: t('User'),
      cell: (row) => (
        <div>
          {row.username || '-'}
          <p className='text-muted-foreground text-xs'>ID: {row.user_id}</p>
        </div>
      ),
    },
    {
      id: 'order',
      header: t('Order number'),
      cell: (row) => (
        <Button
          variant='link'
          className='max-w-64 truncate'
          onClick={() => setSelected(row.trade_no)}
        >
          {row.trade_no}
        </Button>
      ),
    },
    {
      id: 'provider',
      header: t('Payment method'),
      cell: (row) => providers[row.payment_provider] || row.payment_method,
    },
    {
      id: 'credit',
      header: t('Balance credit'),
      cell: (row) =>
        row.credit_quota
          ? formatQuotaWithCurrency(row.credit_quota)
          : formatNumber(row.amount, locale),
    },
    {
      id: 'money',
      header: t('Amount due'),
      cell: (row) =>
        new Intl.NumberFormat(locale, {
          minimumFractionDigits: 2,
          maximumFractionDigits: 2,
        }).format(row.money),
    },
    {
      id: 'actual',
      header: t('Amount received'),
      cell: (row) =>
        row.paid_time
          ? new Intl.NumberFormat(locale, {
              minimumFractionDigits: 2,
              maximumFractionDigits: 2,
            }).format(row.actual_money)
          : '-',
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) => statuses[row.status] || row.status,
    },
    {
      id: 'time',
      header: t('Created at'),
      cell: (row) => formatTimestamp(row.create_time),
    },
    { id: 'ip', header: t('IP address'), cell: (row) => row.create_ip || '-' },
    {
      id: 'actions',
      header: t('Actions'),
      cell: (order) => (
        <div className='flex items-center gap-2'>
          <Button
            size='sm'
            variant='outline'
            onClick={() => setSelected(order.trade_no)}
          >
            {t('Details')}
          </Button>
          {renderPaymentActions(order)}
        </div>
      ),
    },
  ]
  const row = detail.data
  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Order logs')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            <div className='flex flex-wrap items-center gap-2'>
              <Input
                aria-label={t('Search orders')}
                className='w-full sm:w-72'
                placeholder={t('Username, user ID or order number')}
                maxLength={128}
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    setSearch(keyword)
                    setPage(1)
                  }
                }}
              />
              <Button
                onClick={() => {
                  setSearch(keyword)
                  setPage(1)
                }}
              >
                {t('Search')}
              </Button>
              <Select
                items={statusItems}
                value={status}
                onValueChange={(value) => {
                  setStatus(value || 'all')
                  setPage(1)
                }}
              >
                <SelectTrigger aria-label={t('Status')} className='w-40'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All statuses')}</SelectItem>
                  {Object.entries(statuses).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                items={providerItems}
                value={provider}
                onValueChange={(value) => {
                  setProvider(value || 'all')
                  setPage(1)
                }}
              >
                <SelectTrigger
                  aria-label={t('Payment method')}
                  className='w-44'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>
                    {t('All payment methods')}
                  </SelectItem>
                  {Object.entries(providers).map(([value, label]) => (
                    <SelectItem key={value} value={value}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                variant='outline'
                disabled={list.isFetching}
                onClick={() => void list.refetch()}
              >
                {t('Refresh')}
              </Button>
            </div>
            {list.isError ? (
              <p role='alert'>{t('Failed to load orders')}</p>
            ) : null}
            <StaticDataTable
              columns={columns}
              data={list.data?.items || []}
              getRowKey={(row) => row.id}
              emptyContent={list.isPending ? t('Loading...') : t('No orders')}
            />
          </div>
          <PageFooterPortal>
            <div className='flex flex-wrap items-center justify-end gap-3'>
              <span>
                {t('Total {{count}} orders', { count: list.data?.total || 0 })}
              </span>
              <Button
                variant='outline'
                disabled={page === 1 || list.isFetching}
                onClick={() => setPage(page - 1)}
              >
                {t('Previous page')}
              </Button>
              <span>{formatNumber(page, locale)}</span>
              <Button
                variant='outline'
                disabled={
                  page * 20 >= (list.data?.total || 0) || list.isFetching
                }
                onClick={() => setPage(page + 1)}
              >
                {t('Next page')}
              </Button>
            </div>
          </PageFooterPortal>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <Dialog
        open={Boolean(selected)}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('Order details')}
        footer={row ? renderPaymentActions(row) : undefined}
      >
        {detail.isPending ? t('Loading...') : null}
        {detail.isError ? (
          <p role='alert'>{t('Failed to load order')}</p>
        ) : null}
        {row ? (
          <div className='space-y-4'>
            <div className='flex items-center gap-2 break-all'>
              {row.trade_no}
              <CopyButton value={row.trade_no} />
            </div>
            <dl className='grid grid-cols-2 gap-3 text-sm'>
              <dt>{t('Provider transaction number')}</dt>
              <dd className='break-all'>{row.provider_trade_no || '-'}</dd>
              <dt>{t('Status')}</dt>
              <dd>{statuses[row.status] || row.status}</dd>
              <dt>{t('Created at')}</dt>
              <dd>{formatTimestamp(row.create_time)}</dd>
              <dt>{t('Paid at')}</dt>
              <dd>{row.paid_time ? formatTimestamp(row.paid_time) : '-'}</dd>
              <dt>{t('Credited at')}</dt>
              <dd>
                {row.complete_time ? formatTimestamp(row.complete_time) : '-'}
              </dd>
              <dt>{t('Closed at')}</dt>
              <dd>
                {row.closed_time ? formatTimestamp(row.closed_time) : '-'}
              </dd>
              <dt>{t('IP address')}</dt>
              <dd>{row.create_ip || '-'}</dd>
            </dl>
            {row.last_error ? (
              <p className='text-destructive'>{row.last_error}</p>
            ) : null}
            <h3 className='font-medium'>{t('Processing history')}</h3>
            <ul className='space-y-2 text-sm'>
              {row.events.map((event) => (
                <li key={event.id} className='break-words'>
                  {formatTimestamp(event.created_at)} · {event.message}
                  {event.actor_id ? ` · #${event.actor_id}` : ''}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </Dialog>
      <ConfirmDialog
        open={Boolean(action)}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setAction(null)
        }}
        title={
          action?.type === 'retry' ? t('Retry callback') : t('Close order')
        }
        desc={
          action?.type === 'retry'
            ? t('Reconcile the receipt and retry crediting this order.')
            : t(
                'Closing this order does not refund a payment. Do not pay after closing.'
              )
        }
        destructive={action?.type === 'close'}
        isLoading={mutation.isPending}
        handleConfirm={() => {
          if (action && !mutation.isPending) mutation.mutate(action)
        }}
      >
        <p className='text-sm break-all'>{action?.tradeNo}</p>
      </ConfirmDialog>
    </>
  )
}
