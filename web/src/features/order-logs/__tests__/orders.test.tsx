import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { OrderLogs } from '..'

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})
test('订单空列表可搜索用户名和订单号，分页按总数禁用', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { items: [], total: 0 } },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <OrderLogs />
    </QueryClientProvider>
  )
  expect(await screen.findByText('No orders')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
  fireEvent.change(screen.getByRole('textbox', { name: 'Search orders' }), {
    target: { value: 'receipt-a' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(get).toHaveBeenLastCalledWith('/api/user/order-logs', {
      params: { keyword: 'receipt-a', status: '', provider: '', page: 1 },
    })
  )
  client.clear()
})
