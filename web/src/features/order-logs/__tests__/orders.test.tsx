import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import zhCN from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { OrderLogs } from '..'

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

const receipt = {
  id: 1,
  user_id: 2,
  username: 'receipt-user',
  trade_no: 'receipt-order',
  provider_trade_no: '',
  payment_provider: 'alipay_receipt',
  payment_method: 'alipay',
  amount: 1,
  credit_quota: 500000,
  money: 7.01,
  actual_money: 0,
  create_time: 1792112400,
  paid_time: 0,
  complete_time: 0,
  closed_time: 0,
  create_ip: '192.0.2.1',
  status: 'pending',
  last_error: '',
}

async function renderOrders(
  role: number = ROLE.SUPER_ADMIN,
  terminalStatuses: string[] = []
) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'operator', role })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [
          receipt,
          {
            ...receipt,
            id: 2,
            trade_no: 'epay-order',
            payment_provider: 'epay',
          },
          {
            ...receipt,
            id: 3,
            trade_no: 'paid-order',
            status: 'success',
            paid_time: 0,
            actual_money: 6.8,
          },
          {
            ...receipt,
            id: 4,
            trade_no: 'crediting-order',
            paid_time: 1792112500,
          },
          ...terminalStatuses.map((status, index) => ({
            ...receipt,
            id: 5 + index,
            status,
            trade_no: `${status}-order`,
          })),
        ],
        total: 4,
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const i18n = createInstance()
  await i18n.init({
    lng: 'zhCN',
    fallbackLng: 'en',
    resources: { zhCN, en },
    nsSeparator: false,
  })
  const result = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <OrderLogs />
      </QueryClientProvider>
    </I18nextProvider>
  )
  await screen.findByRole('button', { name: 'receipt-order' })
  return { client, ...result }
}

test('中文页面展示订单标题和筛选名称，不显示内部枚举值', async () => {
  const { client } = await renderOrders()
  expect(screen.getByRole('heading', { name: '订单日志' })).toBeVisible()
  expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent(
    '全部状态'
  )
  expect(screen.getByRole('combobox', { name: '支付通道' })).toHaveTextContent(
    '全部支付通道'
  )
  for (const name of ['订单号', '创建时间', 'IP 地址', '操作']) {
    expect(screen.getByRole('columnheader', { name })).toBeVisible()
  }
  client.clear()
})

test.each([
  ['补回调', 'retry'],
  ['关闭订单', 'close'],
])(
  '根用户在新收款码订单行点击%s，确认后只提交所选订单',
  async (label, action) => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true } })
    const { client } = await renderOrders()
    const orderRow = screen.getByRole('row', { name: /receipt-order/ })
    fireEvent.click(within(orderRow).getByRole('button', { name: label }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText('receipt-order')).toBeVisible()
    expect(post).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: '继续' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        `/api/user/order-logs/receipt-order/${action}`
      )
    )
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    client.clear()
  }
)

test('补回调核验失败后保留确认框，取消不会重发请求', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: false, message: '未发现匹配收款' } })
  const { client } = await renderOrders()
  fireEvent.click(
    within(screen.getByRole('row', { name: /receipt-order/ })).getByRole(
      'button',
      { name: '补回调' }
    )
  )
  const dialog = await screen.findByRole('alertdialog')
  fireEvent.click(within(dialog).getByRole('button', { name: '继续' }))
  await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
  await waitFor(() =>
    expect(within(dialog).getByRole('button', { name: '取消' })).toBeEnabled()
  )
  fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(post).toHaveBeenCalledTimes(1)
  client.clear()
})

test('旧通道不提供补回调，已到账订单不再操作，已收款订单不能关闭', async () => {
  const { client } = await renderOrders()
  for (const trade of ['epay-order', 'paid-order']) {
    const row = screen.getByRole('row', { name: new RegExp(trade) })
    expect(
      within(row).queryByRole('button', { name: '补回调' })
    ).not.toBeInTheDocument()
    expect(
      within(row).queryByRole('button', { name: '人工补单' })
    ).not.toBeInTheDocument()
  }
  const row = screen.getByRole('row', { name: /crediting-order/ })
  expect(within(row).getByRole('button', { name: '关闭订单' })).toBeDisabled()
  // 人工补单没有支付宝账务时间时，也应展示已登记的实收金额。
  expect(screen.getByRole('row', { name: /paid-order/ })).toHaveTextContent(
    '6.80'
  )
  client.clear()
})

test('普通管理员只可查看订单，不出现补回调和关闭操作', async () => {
  const { client } = await renderOrders(ROLE.ADMIN)
  expect(
    screen.queryByRole('button', { name: '补回调' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: '关闭订单' })
  ).not.toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: '详情' })).toHaveLength(4)
  expect(
    screen.queryByRole('button', { name: '人工补单' })
  ).not.toBeInTheDocument()
  client.clear()
})

test('人工补单校验资料并二次确认，按原额度提交所选订单且不发送可篡改额度', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const { client } = await renderOrders()
  fireEvent.click(
    within(screen.getByRole('row', { name: /receipt-order/ })).getByRole(
      'button',
      { name: '人工补单' }
    )
  )
  const dialog = await screen.findByRole('dialog', { name: '人工补单' })
  expect(within(dialog).getByText('receipt-order')).toBeVisible()
  fireEvent.click(within(dialog).getByRole('button', { name: '核对补单' }))
  await waitFor(() =>
    expect(
      within(dialog).getByRole('textbox', { name: '实收金额' })
    ).toHaveAttribute('aria-invalid', 'true')
  )
  expect(post).not.toHaveBeenCalled()
  fireEvent.change(within(dialog).getByRole('textbox', { name: '实收金额' }), {
    target: { value: '6.80' },
  })
  fireEvent.change(
    within(dialog).getByRole('textbox', { name: '支付宝交易号' }),
    { target: { value: '202610170000000000000001' } }
  )
  fireEvent.change(within(dialog).getByRole('textbox', { name: '补单原因' }), {
    target: { value: '用户付错金额，已核对' },
  })
  fireEvent.click(within(dialog).getByRole('button', { name: '核对补单' }))
  const confirmation = await screen.findByRole('alertdialog', {
    name: '确认人工补单',
  })
  expect(confirmation).toHaveTextContent('按原订单额度')
  expect(confirmation).toHaveTextContent('6.80')
  expect(post).not.toHaveBeenCalled()
  fireEvent.click(
    within(confirmation).getByRole('button', { name: '确认到账' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledExactlyOnceWith(
      '/api/user/order-logs/receipt-order/manual-credit',
      {
        actual_money: '6.80',
        provider_trade_no: '202610170000000000000001',
        reason: '用户付错金额，已核对',
      }
    )
  )
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: '人工补单' })
    ).not.toBeInTheDocument()
  )
  client.clear()
})

test.each(['expired', 'closed'])(
  '根用户可补%s订单，失败展示原因且保留输入',
  async (status) => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: false, message: '该交易号已用于其他订单' },
    })
    const { client } = await renderOrders(ROLE.SUPER_ADMIN, [status])
    fireEvent.click(
      within(
        screen.getByRole('row', { name: new RegExp(`${status}-order`) })
      ).getByRole('button', { name: '人工补单' })
    )
    const dialog = await screen.findByRole('dialog', { name: '人工补单' })
    fireEvent.change(
      within(dialog).getByRole('textbox', { name: '实收金额' }),
      { target: { value: '7' } }
    )
    fireEvent.change(
      within(dialog).getByRole('textbox', { name: '支付宝交易号' }),
      { target: { value: '202610170000000000000002' } }
    )
    fireEvent.change(
      within(dialog).getByRole('textbox', { name: '补单原因' }),
      { target: { value: '补单核对' } }
    )
    fireEvent.click(within(dialog).getByRole('button', { name: '核对补单' }))
    const confirmation = await screen.findByRole('alertdialog')
    fireEvent.click(
      within(confirmation).getByRole('button', { name: '确认到账' })
    )
    expect(await within(confirmation).findByRole('alert')).toHaveTextContent(
      '该交易号已用于其他订单'
    )
    fireEvent.click(within(confirmation).getByRole('button', { name: '取消' }))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(
      within(dialog).getByRole('textbox', { name: '补单原因' })
    ).toHaveValue('补单核对')
    expect(post).toHaveBeenCalledTimes(1)
    client.clear()
  }
)

test('订单内容使用页面滚动区，分页固定在滚动区域外', async () => {
  const { client } = await renderOrders()
  const main = screen.getByRole('main')
  const scrollArea = main.querySelector('.overflow-auto')
  expect(scrollArea).toContainElement(screen.getByRole('table'))
  expect(scrollArea).not.toContainElement(
    screen.getByRole('button', { name: '下一页' })
  )
  expect(main).toContainElement(screen.getByRole('button', { name: '下一页' }))
  client.clear()
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
  expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled()
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
