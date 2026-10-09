/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { defaultAsyncMediaRetry } from '../async-media-retry-form'
import { AsyncMediaRetrySection } from '../async-media-retry-section'
import { getPolicySectionNavItems } from '../section-registry'

let client: QueryClient
let actions: HTMLDivElement
let channels = [
  { id: 12, name: 'Banana 主渠道', status: 1 },
  { id: 38, name: 'Video 备用', status: 2 },
]

function mount(value = JSON.stringify(defaultAsyncMediaRetry)) {
  const tree = (config: string) => (
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <AsyncMediaRetrySection value={config} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  const view = render(tree(value))
  return (config: string) => view.rerender(tree(config))
}

beforeEach(() => {
  // 只补充 jsdom 缺失的动画查询，滚动布局仍在真实浏览器验证。
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  actions = document.createElement('div')
  document.body.append(actions)
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, user: { id: 1, username: 'root', role: 100 } },
  }))
  channels = [
    { id: 12, name: 'Banana 主渠道', status: 1 },
    { id: 38, name: 'Video 备用', status: 2 },
  ]
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: channels },
  }))
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  vi.spyOn(toast, 'success').mockImplementation(() => 1)
  vi.spyOn(toast, 'error').mockImplementation(() => 1)
})

afterEach(() => {
  cleanup()
  client.clear()
  actions.remove()
})

describe('媒体换渠道重试设置', () => {
  it.each([
    ['502,503', '502,503'],
    ['429,502,503,504', '429,502,503,504'],
    ['429,502-504,503', '429,502,503,504'],
    [' 503，502，503，504，429 ', '429,502,503,504'],
  ])('错误码 %s 保存和重新加载后逐个显示', async (input, expected) => {
    const rerender = mount()
    const field = await screen.findByLabelText(
      'Retry HTTP error codes — Banana 主渠道 #12'
    )
    fireEvent.change(field, { target: { value: input } })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    const body = vi.mocked(api.put).mock.calls[0][1] as { value: string }
    expect(JSON.parse(body.value).channel_status_codes['12']).toBe(expected)
    await waitFor(() => expect(field).toHaveValue(expected))
    await act(async () => rerender(body.value))
    expect(field).toHaveValue(expected)
  })

  it('加载旧区间配置时逐个显示错误码且不自动保存', async () => {
    mount(
      JSON.stringify({
        ...defaultAsyncMediaRetry,
        channel_ids: [12],
        channel_status_codes: { '12': '429,502-504,503' },
      })
    )
    expect(
      await screen.findByLabelText('Retry HTTP error codes — Banana 主渠道 #12')
    ).toHaveValue('429,502,503,504')
    expect(api.put).not.toHaveBeenCalled()
  })

  it('原渠道重试次数独立保存且不占用换渠道次数', async () => {
    mount()
    await screen.findByRole('checkbox', { name: /Banana/ })
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Same-channel retry limit' }),
      { target: { value: '2' } }
    )
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' }),
      { target: { value: '5' } }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    const body = vi.mocked(api.put).mock.calls[0][1] as { value: string }
    expect(JSON.parse(body.value)).toMatchObject({
      same_channel_retries: 2,
      max_retries: 5,
    })
  })

  it.each(['-1', '6', '1.5'])('原渠道次数 %s 无效时阻止保存', async (value) => {
    mount()
    await screen.findByRole('checkbox', { name: /Banana/ })
    const input = screen.getByRole('spinbutton', {
      name: 'Same-channel retry limit',
    })
    fireEvent.change(input, { target: { value } })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
    expect(api.put).not.toHaveBeenCalled()
  })

  it('只展示一个总开关，直接勾选渠道并保存独立错误码', async () => {
    mount()
    const user = userEvent.setup()
    const banana = await screen.findByLabelText(
      'Retry HTTP error codes — Banana 主渠道 #12'
    )
    expect(screen.getAllByRole('switch')).toHaveLength(1)
    expect(
      screen.queryByText('Limit failover to selected channels')
    ).not.toBeInTheDocument()
    await user.click(
      screen.getByRole('switch', { name: 'Enable media channel failover' })
    )
    await user.click(screen.getByRole('checkbox', { name: /Banana/ }))
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' }),
      { target: { value: '3' } }
    )
    fireEvent.change(banana, { target: { value: '500, 502-503' } })
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    const body = vi.mocked(api.put).mock.calls[0][1] as {
      key: string
      value: string
    }
    expect(body.key).toBe('AsyncMediaRetryPolicy')
    expect(JSON.parse(body.value)).toEqual({
      enabled: true,
      max_retries: 3,
      same_channel_retries: 1,
      channel_status_codes: { '12': '500,502,503' },
      channel_ids: [12],
      selected_channels_only: true,
    })
    expect(toast.success).toHaveBeenCalled()
    expect(getPolicySectionNavItems(i18next.t)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          url: '/system-settings/request-policies/media-retry',
        }),
      ])
    )
  })

  it('非 root 不展示编辑器也不请求渠道摘要', () => {
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, user: { id: 2, username: 'admin', role: 10 } },
    }))
    mount()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalled()
  })

  it.each(['200', '600', 'abc', '503-500'])(
    '渠道错误码 %s 无效时阻止保存',
    async (codes) => {
      mount()
      const field = await screen.findByLabelText(
        'Retry HTTP error codes — Banana 主渠道 #12'
      )
      fireEvent.change(field, { target: { value: codes } })
      await userEvent.click(
        screen.getByRole('button', { name: 'Save Changes' })
      )
      expect(
        await screen.findByText(
          'Enter HTTP error codes from 400 to 599, such as 500 or 500-503.'
        )
      ).toBeVisible()
      expect(field).toHaveAttribute('aria-invalid', 'true')
      expect(api.put).not.toHaveBeenCalled()
    }
  )

  it.each(['-1', '21', '1.5'])('次数 %s 无效时阻止保存', async (value) => {
    mount()
    await screen.findByRole('checkbox', { name: /Banana/ })
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' }),
      { target: { value } }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(
        screen.getByRole('spinbutton', { name: 'Channel switch retry limit' })
      ).toHaveAttribute('aria-invalid', 'true')
    )
    expect(api.put).not.toHaveBeenCalled()
  })

  it('名称和 ID 模糊搜索保留隐藏行勾选及错误码', async () => {
    mount()
    const user = userEvent.setup()
    fireEvent.change(
      await screen.findByLabelText(
        'Retry HTTP error codes — Banana 主渠道 #12'
      ),
      { target: { value: '500' } }
    )
    await user.click(screen.getByRole('checkbox', { name: /Banana/ }))
    const search = screen.getByRole('textbox', {
      name: 'Search channels by name or ID',
    })
    await user.type(search, '38')
    expect(
      screen.queryByRole('checkbox', { name: /Banana/ })
    ).not.toBeInTheDocument()
    fireEvent.change(
      screen.getByLabelText('Retry HTTP error codes — Video 备用 #38'),
      { target: { value: '429,503' } }
    )
    await user.click(screen.getByRole('checkbox', { name: /Video/ }))
    await user.clear(search)
    await user.type(search, 'bAn')
    expect(screen.getByRole('checkbox', { name: /Banana/ })).toBeChecked()
    expect(
      screen.getByLabelText('Retry HTTP error codes — Banana 主渠道 #12')
    ).toHaveValue('500')
    expect(screen.getByLabelText('Retry channel list')).toHaveClass('h-72')
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    const body = vi.mocked(api.put).mock.calls[0][1] as { value: string }
    expect(JSON.parse(body.value).channel_ids).toEqual([12, 38])
    expect(JSON.parse(body.value).channel_status_codes).toEqual({
      '12': '500',
      '38': '429,503',
    })
  })

  it('启用时空名单阻止保存，关闭总开关后可以保存空名单', async () => {
    mount(
      JSON.stringify({
        ...defaultAsyncMediaRetry,
        enabled: true,
        channel_ids: [12],
        channel_status_codes: { '12': '500' },
      })
    )
    await screen.findByRole('checkbox', { name: /Banana/ })
    await userEvent.click(
      screen.getByRole('button', { name: 'Clear selection' })
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Select at least one channel before enabling failover.'
    )
    expect(api.put).not.toHaveBeenCalled()
    await userEvent.click(
      screen.getByRole('switch', { name: 'Enable media channel failover' })
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    const body = vi.mocked(api.put).mock.calls[0][1] as { value: string }
    expect(JSON.parse(body.value)).toMatchObject({
      enabled: false,
      channel_ids: [],
      selected_channels_only: true,
    })
  })

  it('已取消的勾选在重新加载已保存设置后不会自动勾回', async () => {
    mount(
      JSON.stringify({
        ...defaultAsyncMediaRetry,
        channel_status_codes: { '12': '500' },
        channel_ids: [],
      })
    )
    expect(
      await screen.findByRole('checkbox', { name: /Banana/ })
    ).not.toBeChecked()
    expect(
      screen.getByLabelText('Retry HTTP error codes — Banana 主渠道 #12')
    ).toHaveValue('500')
  })

  it('保存后未配置行仍可再次提交，不产生无效输入错误', async () => {
    mount(
      JSON.stringify({
        ...defaultAsyncMediaRetry,
        enabled: true,
        channel_ids: [12],
        channel_status_codes: { '12': '500' },
      })
    )
    await screen.findByRole('checkbox', { name: /Banana/ })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    await userEvent.click(
      screen.getByRole('button', { name: 'Clear selection' })
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Select at least one channel before enabling failover.'
    )
    await userEvent.click(
      screen.getByRole('switch', { name: 'Enable media channel failover' })
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('Invalid input')).not.toBeInTheDocument()
  })

  it('刷新设置保留未保存的勾选和次数草稿', async () => {
    const rerender = mount()
    await userEvent.click(
      await screen.findByRole('checkbox', { name: /Banana/ })
    )
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' }),
      { target: { value: '7' } }
    )
    const codes = screen.getByLabelText(
      'Retry HTTP error codes — Banana 主渠道 #12'
    )
    fireEvent.change(codes, { target: { value: '502, 503' } })
    await act(async () =>
      rerender(
        JSON.stringify({
          ...defaultAsyncMediaRetry,
          max_retries: 5,
          channel_status_codes: { '12': '429,502-504' },
        })
      )
    )
    expect(screen.getByRole('checkbox', { name: /Banana/ })).toBeChecked()
    expect(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' })
    ).toHaveValue(7)
    expect(codes).toHaveValue('502, 503')
  })

  it('保存失败保留草稿并展示服务端错误', async () => {
    vi.mocked(api.put).mockResolvedValue({
      data: { success: false, message: '设置保存失败' },
    })
    mount()
    await screen.findByRole('checkbox', { name: /Banana/ })
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' }),
      { target: { value: '4' } }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('设置保存失败')
    )
    expect(toast.success).not.toHaveBeenCalled()
    expect(
      screen.getByRole('spinbutton', { name: 'Channel switch retry limit' })
    ).toHaveValue(4)
  })

  it('渠道加载失败禁用保存，重新加载后可以清除已删除渠道', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('offline'))
    mount(JSON.stringify({ ...defaultAsyncMediaRetry, channel_ids: [12] }))
    expect(await screen.findByText('Failed to load channels')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled()
    vi.mocked(api.get).mockResolvedValue({ data: { success: true, data: [] } })
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('No channels found')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  })

  it('启用时勾选的渠道必须填写错误码', async () => {
    mount(
      JSON.stringify({
        ...defaultAsyncMediaRetry,
        enabled: true,
        channel_ids: [12, 38],
        channel_status_codes: { '12': '500' },
      })
    )
    await screen.findByRole('checkbox', { name: /Video/ })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(
      await screen.findByText(
        'Enter HTTP error codes from 400 to 599, such as 500 or 500-503.'
      )
    ).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  })

  it('旧不限渠道配置转换成明确勾选，保留各渠道已有错误码', async () => {
    mount(
      JSON.stringify({
        enabled: true,
        max_retries: 2,
        status_codes: '502-503',
        channel_ids: [],
      })
    )
    const banana = await screen.findByRole('checkbox', { name: /Banana/ })
    await waitFor(() => expect(banana).toBeChecked())
    expect(screen.getByRole('checkbox', { name: /Video/ })).toBeChecked()
    fireEvent.change(
      screen.getByLabelText('Retry HTTP error codes — Banana 主渠道 #12'),
      { target: { value: '429' } }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    const body = vi.mocked(api.put).mock.calls[0][1] as { value: string }
    expect(JSON.parse(body.value)).toMatchObject({
      channel_ids: [12, 38],
      channel_status_codes: { '12': '429', '38': '502,503' },
      selected_channels_only: true,
    })
    expect(JSON.parse(body.value)).not.toHaveProperty('status_codes')
  })
})
