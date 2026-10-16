import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { AlipayReceiptSettings } from '../alipay-receipt-settings'

afterEach(() => vi.restoreAllMocks())
test('个人日限额可保存，非整数不提交，已配置私钥不回显', async () => {
  const config = {
    enabled: false,
    account_id: '2088000000000001',
    app_id: '2026000000000001',
    qr_code: 'https://qr.alipay.com/fixture',
    private_key: '',
    public_key: 'public-fixture',
    daily_limit: 10,
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        config,
        private_key_configured: true,
        last_scan_at: 0,
        last_error: '',
      },
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <AlipayReceiptSettings />
    </QueryClientProvider>
  )
  const daily = await screen.findByRole('spinbutton', {
    name: 'Daily orders per user',
  })
  await waitFor(() =>
    expect(screen.getByLabelText('Alipay account UID')).toHaveValue(
      config.account_id
    )
  )
  expect(screen.getByLabelText('Application private key')).toHaveValue('')
  fireEvent.change(daily, { target: { value: '2.5' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(daily).toHaveAttribute('aria-invalid', 'true'))
  expect(post).not.toHaveBeenCalled()
  fireEvent.change(daily, { target: { value: '3' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/option/alipay-receipt', {
      ...config,
      daily_limit: 3,
    })
  )
  client.clear()
})
