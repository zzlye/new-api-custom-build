import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
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
import { lotteryTimeStamp, lotteryTimeValue } from '../lottery-time'
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
  topup_scope: 'lifetime',
  topup_start: 0,
  topup_end: 0,
  per_user_limit: 2,
  used_draws: 0,
  eligible_topup_quota: 10000,
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
  vi.spyOn(api, 'delete').mockResolvedValue({ data: { success: true } })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.endsWith('/winners')) {
      return {
        data: {
          success: true,
          data: [{ id: 1, username: 'p***t', quota: 100 }],
        },
      }
    }
    return {
      data: {
        success: true,
        data: url.endsWith('/draws')
          ? { items: [], total: 0 }
          : { items: [{ ...activity }], total: 1, paid_topup_quota: 10000 },
      },
    }
  })
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
  it('用户页不展示奖池金额和剩余份数，保留个人抽奖入口', async () => {
    vi.mocked(api.get).mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/winners')
          ? []
          : {
              items: [
                {
                  ...activity,
                  total_quota: 123456,
                  remaining_quota: 9188,
                  remaining_shares: 17,
                },
              ],
              total: 1,
            },
      },
    }))
    render(fixture(<LotteryPage />))
    const stage = await screen.findByRole('region', { name: '秋日抽奖' })
    expect(stage).not.toHaveTextContent('剩余奖池')
    expect(stage).not.toHaveTextContent('总奖池')
    expect(stage).not.toHaveTextContent('91.88')
    expect(stage).not.toHaveTextContent('1,234.56')
    expect(stage).not.toHaveTextContent('17 份')
    expect(
      within(stage).getByRole('button', { name: '立即抽奖' })
    ).toBeEnabled()
    expect(
      within(stage).getByRole('button', { name: '抽奖记录' })
    ).toBeVisible()
  })

  it('root管理页仍显示奖池用于管理', async () => {
    render(fixture(<LotterySettings />))
    expect(await screen.findByText('秋日抽奖')).toBeVisible()
    expect(screen.getByText('剩余 / 总奖池')).toBeVisible()
  })

  it('只展示一个完整活动，切换历史活动不覆盖当前活动', async () => {
    const user = userEvent.setup()
    const old = { ...activity, id: 2, name: '上期抽奖', status: 'ended' }
    vi.mocked(api.get).mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/winners')
          ? []
          : { items: [activity, old], total: 2 },
      },
    }))
    render(fixture(<LotteryPage />))
    expect(
      await screen.findByRole('region', { name: '秋日抽奖' })
    ).toBeVisible()
    expect(screen.queryByText('固定份数拼手气')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('region', { name: '上期抽奖' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('combobox', { name: '切换活动' }))
    await user.click(await screen.findByRole('option', { name: '上期抽奖' }))
    expect(
      await screen.findByRole('region', { name: '上期抽奖' })
    ).toBeVisible()
    expect(
      screen.queryByRole('region', { name: '秋日抽奖' })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '立即抽奖' })).toBeDisabled()
  })

  it('真实中奖栏可以暂停继续，复制滚动项不重复读屏', async () => {
    const user = userEvent.setup()
    render(fixture(<LotteryPage />))
    const ticker = await screen.findByRole('region', { name: '最新中奖' })
    await within(ticker).findByRole('button', { name: '暂停滚动' })
    expect(within(ticker).getAllByRole('listitem')).toHaveLength(1)
    expect(within(ticker).getByRole('listitem')).toHaveTextContent('p***t')
    await user.click(within(ticker).getByRole('button', { name: '暂停滚动' }))
    expect(
      within(ticker).getByRole('button', { name: '继续滚动' })
    ).toHaveAttribute('aria-pressed', 'true')
    await user.click(within(ticker).getByRole('button', { name: '继续滚动' }))
    expect(
      within(ticker).getByRole('button', { name: '暂停滚动' })
    ).toHaveAttribute('aria-pressed', 'false')
  })

  it('时段门槛展示当前时段额度而非历史累计，未达门槛不能抽', async () => {
    vi.mocked(api.get).mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/winners')
          ? []
          : {
              items: [
                {
                  ...activity,
                  topup_scope: 'period',
                  topup_start: 1792425600,
                  topup_end: 1792511999,
                  eligible_topup_quota: 9000,
                },
              ],
              total: 1,
              paid_topup_quota: 999999,
            },
      },
    }))
    render(fixture(<LotteryPage />))
    expect(await screen.findByText('我的时段充值')).toBeVisible()
    expect(screen.queryByText('我的累计充值')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '立即抽奖' })).toBeDisabled()
    expect(await screen.findByText('暂无中奖记录')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: '暂停滚动' })
    ).not.toBeInTheDocument()
  })

  it('指定时段必须填写有效范围，保存按北京时间换算且切回累计清除范围', async () => {
    const user = userEvent.setup()
    const view = render(fixture(<LotteryForm onClose={vi.fn()} />))
    await user.type(
      screen.getByRole('textbox', { name: '活动名称' }),
      '时段活动'
    )
    await user.click(screen.getByRole('combobox', { name: '充值门槛' }))
    await user.click(
      await screen.findByRole('option', { name: '指定时间段充值' })
    )
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByText('请选择有效的充值起止时间')).toBeVisible()
    expect(api.post).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('充值开始时间（北京时间）'), {
      target: { value: '2026-10-20T00:00:00' },
    })
    fireEvent.change(screen.getByLabelText('充值结束时间（北京时间）'), {
      target: { value: '2026-10-20T23:59:59' },
    })
    expect(
      lotteryTimeStamp(
        (screen.getByLabelText('充值开始时间（北京时间）') as HTMLInputElement)
          .value
      )
    ).toBe(1792425600)
    expect(
      lotteryTimeStamp(
        (screen.getByLabelText('充值结束时间（北京时间）') as HTMLInputElement)
          .value
      )
    ).toBe(1792511999)
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() =>
      expect(api.post).toHaveBeenCalledWith(
        '/api/lottery',
        expect.objectContaining({
          topup_scope: 'period',
          topup_start: 1792425600,
          topup_end: 1792511999,
        })
      )
    )
    view.unmount()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: activity } })
    render(
      fixture(
        <LotteryForm
          activity={{
            ...activity,
            status: 'draft',
            topup_scope: 'period',
            topup_start: 1792425600,
            topup_end: 1792511999,
          }}
          onClose={vi.fn()}
        />
      )
    )
    await user.click(screen.getByRole('combobox', { name: '充值门槛' }))
    await user.click(await screen.findByRole('option', { name: '累计充值' }))
    expect(
      screen.queryByLabelText('充值开始时间（北京时间）')
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(
        '/api/lottery/1',
        expect.objectContaining({
          topup_scope: 'lifetime',
          topup_start: 0,
          topup_end: 0,
        })
      )
    )
  })

  it('root只能删除旧活动，取消不发请求，失败保留确认框后可重试', async () => {
    const user = userEvent.setup()
    vi.mocked(api.get).mockResolvedValue({
      data: {
        success: true,
        data: {
          items: [
            activity,
            { ...activity, id: 2, name: '旧活动', status: 'ended' },
          ],
          total: 2,
        },
      },
    })
    vi.mocked(api.delete).mockResolvedValueOnce({
      data: { success: false, message: '删除失败' },
    })
    render(fixture(<LotterySettings />))
    expect(await screen.findAllByRole('button', { name: '删除' })).toHaveLength(
      1
    )
    await user.click(screen.getByRole('button', { name: '删除' }))
    let dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(api.delete).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '删除' }))
    dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(api.delete).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(within(dialog).getByRole('button', { name: '删除' })).toBeEnabled()
    )
    expect(dialog).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: '删除' }))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(api.delete).toHaveBeenLastCalledWith('/api/lottery/2')
  })

  it('北京时间转换拒绝无效日期且不依赖浏览器本地时区', () => {
    expect(lotteryTimeStamp('2026-10-20T00:00')).toBe(1792425600)
    expect(lotteryTimeValue(1792425600)).toBe('2026-10-20T00:00:00')
    expect(lotteryTimeStamp('2026-02-30T00:00')).toBe(0)
    expect(lotteryTimeStamp('')).toBe(0)
  })

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
      vi.mocked(api.get).mockImplementation(async (url) => {
        if (url.endsWith('/winners')) {
          return { data: { success: true, data: [] } }
        }
        return {
          data: {
            success: true,
            data: {
              items: [
                { ...activity, status, used_draws, eligible_topup_quota: paid },
              ],
              total: 1,
              paid_topup_quota: paid,
            },
          },
        }
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
      topup_scope: 'lifetime',
      topup_start: 0,
      topup_end: 0,
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
