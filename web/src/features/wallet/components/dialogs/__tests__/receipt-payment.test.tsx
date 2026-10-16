import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ReceiptPaymentDialog } from '../receipt-payment-dialog'

const order = {
  trade_no: 'AR-fixture',
  status: 'pending',
  paid_time: 0,
  receipt: {
    amount_cents: 1001,
    credit_quota: 5005000,
    confirmed_at: 0,
    expires_at: Math.floor(Date.now() / 1000) + 180,
    qr_code: '',
  },
}
// jsdom 不提供画布，像素与扫码结果由真实浏览器回归验证。
beforeEach(() => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
})
afterEach(() => vi.restoreAllMocks())

test('确认入账后通知钱包刷新余额', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { ...order, status: 'success' } },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const onPaid = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <ReceiptPaymentDialog
        tradeNo='AR-fixture'
        onClose={() => {}}
        onPaid={onPaid}
      />
    </QueryClientProvider>
  )
  expect(await screen.findByText('Payment credited')).toBeVisible()
  await waitFor(() => expect(onPaid).toHaveBeenCalledTimes(1))
  client.clear()
})

test('确认最终金额才显示二维码，关闭弹窗不关闭后台订单', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: order },
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...order,
        receipt: {
          ...order.receipt,
          confirmed_at: 1,
          qr_code: 'https://qr.alipay.com/fixture',
        },
      },
    },
  })
  const onClose = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ReceiptPaymentDialog tradeNo='AR-fixture' onClose={onClose} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('¥10.01')).toBeVisible()
  expect(document.querySelector('canvas[role="img"]')).toBeNull()
  fireEvent.click(
    screen.getByRole('button', { name: 'Confirm amount and show code' })
  )
  const qr = await screen.findByRole('img', { name: 'Alipay receipt code' })
  expect(qr.parentElement).toHaveClass('payment-qr-code')
  expect(
    screen.getByText('Enter the exact amount shown, including cents.')
  ).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/user/alipay-receipt/order/AR-fixture/confirm'
  )
  fireEvent.click(screen.getAllByRole('button', { name: 'Close' })[0])
  expect(onClose).toHaveBeenCalled()
  expect(post).not.toHaveBeenCalledWith(
    '/api/user/alipay-receipt/order/AR-fixture/close'
  )
  client.clear()
})

test('超时订单不展示二维码或确认付款按钮', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...order,
        receipt: {
          ...order.receipt,
          expires_at: 1,
          qr_code: 'https://qr.alipay.com/expired',
        },
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ReceiptPaymentDialog tradeNo='AR-fixture' onClose={() => {}} />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText('Payment window ended. Do not pay this order.')
  ).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Confirm amount and show code' })
  ).toBeNull()
  expect(document.querySelector('svg title')).toBeNull()
  client.clear()
})
