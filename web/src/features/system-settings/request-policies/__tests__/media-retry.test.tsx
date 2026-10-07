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
  it('root 能配置次数与必填错误码并一次保存完整策略', async () => {
    mount()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('switch', { name: 'Enable media channel failover' })
    )
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Retry HTTP error codes'), {
      target: { value: '500, 502-503' },
    })
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
      status_codes: '500,502-503',
      channel_ids: [],
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

  it.each(['', '200', '600', 'abc', '503-500'])(
    '错误码 %s 无效时阻止保存',
    async (codes) => {
      mount()
      fireEvent.change(screen.getByLabelText('Retry HTTP error codes'), {
        target: { value: codes },
      })
      await userEvent.click(
        screen.getByRole('button', { name: 'Save Changes' })
      )
      expect(
        await screen.findByText(
          'Enter HTTP error codes from 400 to 599, such as 500 or 500-503.'
        )
      ).toBeVisible()
      expect(screen.getByLabelText('Retry HTTP error codes')).toHaveAttribute(
        'aria-invalid',
        'true'
      )
      expect(api.put).not.toHaveBeenCalled()
    }
  )

  it.each(['-1', '21', '1.5'])('次数 %s 无效时阻止保存', async (value) => {
    mount()
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value } })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(screen.getByRole('spinbutton')).toHaveAttribute(
        'aria-invalid',
        'true'
      )
    )
    expect(api.put).not.toHaveBeenCalled()
  })

  it('名单支持名称和 ID 模糊搜索，隐藏项保持勾选并保存', async () => {
    mount()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('switch', {
        name: 'Limit failover to selected channels',
      })
    )
    await user.click(await screen.findByRole('checkbox', { name: /Banana/ }))
    const search = screen.getByRole('textbox', {
      name: 'Search channels by name or ID',
    })
    await user.type(search, '38')
    expect(
      screen.queryByRole('checkbox', { name: /Banana/ })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('checkbox', { name: /Video/ }))
    await user.clear(search)
    await user.type(search, 'bAn')
    expect(screen.getByRole('checkbox', { name: /Banana/ })).toBeChecked()
    expect(screen.getByLabelText('Retry channel list')).toHaveClass('h-72')
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalled())
    const body = vi.mocked(api.put).mock.calls[0][1] as { value: string }
    expect(JSON.parse(body.value).channel_ids).toEqual([12, 38])
  })

  it('限定渠道时空名单阻止保存，关闭限定后保存空名单', async () => {
    mount()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('switch', {
        name: 'Limit failover to selected channels',
      })
    )
    await screen.findByRole('checkbox', { name: /Banana/ })
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Select at least one channel'
    )
    expect(api.put).not.toHaveBeenCalled()
    await user.click(
      screen.getByRole('switch', {
        name: 'Limit failover to selected channels',
      })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  })

  it('设置重新加载不覆盖尚未保存的次数和渠道限定', async () => {
    const rerender = mount()
    await userEvent.click(
      screen.getByRole('switch', {
        name: 'Limit failover to selected channels',
      })
    )
    await act(async () =>
      rerender(JSON.stringify({ ...defaultAsyncMediaRetry, max_retries: 4 }))
    )
    expect(
      screen.getByRole('switch', {
        name: 'Limit failover to selected channels',
      })
    ).toBeChecked()
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '7' } })
    await act(async () =>
      rerender(JSON.stringify({ ...defaultAsyncMediaRetry, max_retries: 5 }))
    )
    expect(screen.getByRole('spinbutton')).toHaveValue(7)
  })

  it('保存失败保留草稿并显示服务端错误而非成功提示', async () => {
    vi.mocked(api.put).mockResolvedValue({
      data: { success: false, message: '设置保存失败' },
    })
    mount()
    fireEvent.change(screen.getByRole('spinbutton'), { target: { value: '4' } })
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('设置保存失败')
    )
    expect(toast.success).not.toHaveBeenCalled()
    expect(screen.getByRole('spinbutton')).toHaveValue(4)
  })

  it('渠道加载失败提供重试且不允许保存未知白名单', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('offline'))
    mount(JSON.stringify({ ...defaultAsyncMediaRetry, channel_ids: [12] }))
    expect(await screen.findByText('Failed to load channels')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled()
    vi.mocked(api.get).mockResolvedValue({ data: { success: true, data: [] } })
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('No channels found')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Select at least one channel'
    )
  })
})
