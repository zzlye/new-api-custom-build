import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance, type i18n } from 'i18next'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { LotteryPage } from '..'
import { LotteryForm } from '../lottery-form'
import { LotterySettings } from '../lottery-settings'
import type { LotteryActivity } from '../types'

let client: QueryClient
let language: i18n
const activity: LotteryActivity = {
  id: 1,
  name: '秋日抽奖',
  status: 'active',
  mode: 'random_shares',
  total_quota: 10000,
  remaining_quota: 10000,
  min_topup_quota: 10000,
  per_user_limit: 2,
  used_draws: 0,
  draw_count: 0,
  min_percent: 0,
  max_percent: 0,
  shares: 20,
  remaining_shares: 20,
  fixed_quota: 0,
  created_at: 1792470000,
  started_at: 1792470000,
  ended_at: 0,
}

function fixture(children: ReactNode) {
  return (
    <I18nextProvider i18n={language}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </I18nextProvider>
  )
}

beforeEach(async () => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  language = createInstance()
  await language.init({ lng: 'zh', resources: { zh } })
  useAuthStore
    .getState()
    .auth.setUser({ id: 7, username: 'participant', role: 100 })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 100 } })
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/draws')
        ? { items: [], total: 0 }
        : { items: [{ ...activity }], total: 1, paid_topup_quota: 10000 },
    },
  }))
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: { id: 8, activity_id: 1, user_id: 7, quota: 100 },
    },
  })
})

afterEach(() => {
  client.clear()
  sessionStorage.clear()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  localStorage.clear()
})

describe('抽奖界面', () => {
  it('中文页面展示资格与剩余次数，记录弹窗能够打开关闭', async () => {
    const user = userEvent.setup()
    render(fixture(<LotteryPage />))
    expect(await screen.findByText('秋日抽奖')).toBeVisible()
    expect(screen.getByRole('button', { name: '立即抽奖' })).toBeEnabled()
    expect(screen.getByText('我的累计充值')).toBeVisible()
    await user.click(screen.getByRole('button', { name: '抽奖记录' }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('暂无抽奖记录')).toBeVisible()
    expect(api.get).toHaveBeenCalledWith(
      '/api/user/lottery/1/draws',
      expect.anything()
    )
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
  })

  it.each([
    { status: 'paused', used_draws: 0, paid: 10000 },
    { status: 'active', used_draws: 2, paid: 10000 },
    { status: 'active', used_draws: 0, paid: 9999 },
  ])(
    '活动暂停、次数耗尽或未达到门槛时禁用抽奖：%j',
    async ({ status, used_draws, paid }) => {
      vi.mocked(api.get).mockResolvedValue({
        data: {
          success: true,
          data: {
            items: [{ ...activity, status, used_draws }],
            total: 1,
            paid_topup_quota: paid,
          },
        },
      })
      render(fixture(<LotteryPage />))
      expect(
        await screen.findByRole('button', { name: '立即抽奖' })
      ).toBeDisabled()
      expect(api.post).not.toHaveBeenCalled()
    }
  )

  it('断网后重新打开页面复用请求编号，成功后下一次使用新编号', async () => {
    const user = userEvent.setup()
    vi.mocked(api.post).mockRejectedValueOnce(new Error('network interrupted'))
    const view = render(fixture(<LotteryPage />))
    await user.click(await screen.findByRole('button', { name: '立即抽奖' }))
    await screen.findByRole('button', { name: '获取抽奖结果' })
    const first = vi.mocked(api.post).mock.calls[0][1]
    view.unmount()
    render(fixture(<LotteryPage />))
    await user.click(
      await screen.findByRole('button', { name: '获取抽奖结果' })
    )
    await waitFor(() => expect(api.post).toHaveBeenCalledTimes(2))
    expect(vi.mocked(api.post).mock.calls[1][1]).toEqual(first)
    await user.click(await screen.findByRole('button', { name: '立即抽奖' }))
    await waitFor(() => expect(api.post).toHaveBeenCalledTimes(3))
    expect(vi.mocked(api.post).mock.calls[2][1]).not.toEqual(first)
  })

  it('无活动时显示空状态，接口失败不显示可抽按钮', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({
      data: {
        success: true,
        data: { items: [], total: 0, paid_topup_quota: 0 },
      },
    })
    const view = render(fixture(<LotteryPage />))
    expect(await screen.findByText('暂无抽奖活动')).toBeVisible()
    view.unmount()
    client.clear()
    vi.mocked(api.get).mockRejectedValue(new Error('offline'))
    render(fixture(<LotteryPage />))
    await waitFor(() =>
      expect(client.getQueryState(['lottery', 'user', 1])?.status).toBe('error')
    )
    expect(
      screen.queryByRole('button', { name: '立即抽奖' })
    ).not.toBeInTheDocument()
  })

  it('普通用户不显示管理设置也不请求管理接口', () => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 7, username: 'participant', role: 1 })
    render(fixture(<LotterySettings />))
    expect(
      screen.queryByRole('button', { name: '新建抽奖活动' })
    ).not.toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalled()
  })

  it.each([
    ['初始奖池随机比例', 'initial_percent'],
    ['剩余奖池随机比例', 'remaining_percent'],
    ['固定份数拼手气', 'random_shares'],
    ['固定金额', 'fixed'],
  ])('新建%s按钱包金额换算后保存正确规则', async (label, mode) => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(fixture(<LotteryForm onClose={onClose} />))
    await user.type(
      screen.getByRole('textbox', { name: '活动名称' }),
      '秋日活动'
    )
    await user.click(screen.getByRole('combobox', { name: '发奖方式' }))
    await user.click(await screen.findByRole('option', { name: label }))
    if (mode.endsWith('_percent')) {
      expect(
        screen.getByRole('spinbutton', { name: /最低奖励比例/ })
      ).toBeVisible()
    }
    if (mode === 'fixed') {
      expect(screen.getByRole('spinbutton', { name: /单次奖励/ })).toBeVisible()
    }
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
    expect(api.post).toHaveBeenCalledWith('/api/lottery', {
      name: '秋日活动',
      mode,
      total_quota: 10000,
      min_topup_quota: 10000,
      per_user_limit: 1,
      min_percent: mode.endsWith('_percent') ? 100 : 0,
      max_percent: mode.endsWith('_percent') ? 300 : 0,
      shares: mode === 'random_shares' ? 20 : 0,
      fixed_quota: mode === 'fixed' ? 100 : 0,
    })
  })

  it('非法名称阻止保存，业务保存失败保留表单内容', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(fixture(<LotteryForm onClose={onClose} />))
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByText('请输入活动名称')).toBeVisible()
    expect(api.post).not.toHaveBeenCalled()
    vi.mocked(api.post).mockResolvedValue({
      data: { success: false, message: '保存失败' },
    })
    await user.type(
      screen.getByRole('textbox', { name: '活动名称' }),
      '不能丢失的草稿'
    )
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(client.isMutating()).toBe(0))
    expect(screen.getByRole('textbox', { name: '活动名称' })).toHaveValue(
      '不能丢失的草稿'
    )
    expect(onClose).not.toHaveBeenCalled()
  })
})
