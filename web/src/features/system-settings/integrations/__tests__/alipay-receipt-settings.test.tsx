import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import zhCN from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'

import { AlipayReceiptSettings } from '../alipay-receipt-settings'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
const config = {
  enabled: false,
  account_id: '2088000000000001',
  app_id: '2026000000000001',
  qr_code: 'https://qr.alipay.com/fixture',
  private_key: '',
  public_key: 'public-fixture',
  daily_limit: 10,
}
async function renderSettings(language = 'en') {
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
  const i18n = createInstance()
  await i18n.init({
    lng: language,
    fallbackLng: 'en',
    resources: { en, zhCN },
    nsSeparator: false,
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <AlipayReceiptSettings />
      </QueryClientProvider>
    </I18nextProvider>
  )
  await waitFor(() =>
    expect(screen.getByDisplayValue(config.account_id)).toBeVisible()
  )
  return { post, client }
}

test('个人日限额可保存，非整数不提交，已配置私钥不回显', async () => {
  const { post, client } = await renderSettings()
  const daily = await screen.findByRole('spinbutton', {
    name: 'Daily orders per user',
  })
  await waitFor(() =>
    expect(screen.getByLabelText('Alipay account UID')).toHaveValue(
      config.account_id
    )
  )
  expect(screen.getByLabelText('Application private key')).toHaveValue('')
  expect(
    screen.getByRole('button', { name: 'Choose receipt code image' })
  ).toBeEnabled()
  expect(
    screen.getByRole('img', { name: 'Receipt code preview' })
  ).toBeVisible()
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

test('切换中文后收款码配置和图片入口使用中文', async () => {
  const { client } = await renderSettings('zhCN')
  expect(screen.getByRole('button', { name: '选择收款码图片' })).toBeVisible()
  expect(screen.getByRole('img', { name: '收款码预览' })).toBeVisible()
  expect(screen.getByLabelText('收款码地址')).toHaveValue(config.qr_code)
  expect(screen.getByLabelText('每人每日可创建订单数')).toHaveValue(10)
  expect(screen.getByRole('switch', { name: '启用支付宝收款码' })).toBeVisible()
  client.clear()
})

test('选择非图片或过大图片时保留原收款码，不触发配置保存', async () => {
  const { post, client } = await renderSettings()
  const input = screen.getByLabelText('Alipay receipt code')
  const invalid = new File(['not image'], 'receipt.txt', { type: 'text/plain' })
  fireEvent.change(input, { target: { files: [invalid] } })
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Choose a PNG, JPG or WebP image up to 10 MB'
  )
  const big = new File(['fixture'], 'large.png', { type: 'image/png' })
  Object.defineProperty(big, 'size', { value: 10 * 1024 * 1024 + 1 })
  fireEvent.change(input, { target: { files: [big] } })
  expect(screen.getByLabelText('Receipt code URL')).toHaveValue(config.qr_code)
  expect(post).not.toHaveBeenCalled()
  client.clear()
})

test('损坏图片识别失败时恢复选择按钮并释放临时图片，不覆盖已配置地址', async () => {
  const revoke = vi.fn()
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL = vi.fn(() => 'blob:receipt-fixture')
      static revokeObjectURL = revoke
    }
  )
  vi.stubGlobal(
    'Image',
    class {
      src = ''
      decode = () => Promise.reject(new Error('invalid-image'))
    }
  )
  const { client, post } = await renderSettings()
  fireEvent.change(screen.getByLabelText('Alipay receipt code'), {
    target: {
      files: [new File(['broken'], 'broken.png', { type: 'image/png' })],
    },
  })
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Unable to read this image'
  )
  expect(screen.getByLabelText('Receipt code URL')).toHaveValue(config.qr_code)
  expect(
    screen.getByRole('button', { name: 'Choose receipt code image' })
  ).toBeEnabled()
  expect(revoke).toHaveBeenCalledWith('blob:receipt-fixture')
  expect(post).not.toHaveBeenCalled()
  client.clear()
})

test.each([
  'https://qr.alipay.com.example.org/pay',
  'https://qr.alipay.com@evil.example/pay',
  'https://example.org/code.png',
  'javascript:alert(1)',
])('地址 %s 不是支付宝收款码时不允许保存也不展示付款预览', async (url) => {
  const { client, post } = await renderSettings()
  fireEvent.change(screen.getByLabelText('Receipt code URL'), {
    target: { value: url },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(
    await screen.findByText('Please use an Alipay receipt code URL')
  ).toBeVisible()
  expect(
    screen.queryByRole('img', { name: 'Receipt code preview' })
  ).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
  client.clear()
})
