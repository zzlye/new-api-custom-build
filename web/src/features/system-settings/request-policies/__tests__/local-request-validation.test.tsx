import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
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

import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { LocalRequestValidationSection } from '../local-request-validation-section'

let client: QueryClient
let actions: HTMLDivElement
const empty = '{"enabled":false,"rules":[]}'

// 挂载本地参数校验与错误拦截设置组件
function mount(value = empty) {
  const tree = (config: string) => (
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <LocalRequestValidationSection value={config} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  const view = render(tree(value))
  return (config: string) => view.rerender(tree(config))
}

beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  actions = document.createElement('div')
  document.body.append(actions)
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, user: { id: 1, username: 'root', role: 100 } },
  }))
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: ['gemini', 'gemini.pro', 'default'] },
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  vi.spyOn(toast, 'success').mockImplementation(() => 1)
  vi.spyOn(toast, 'error').mockImplementation(() => 1)
})

afterEach(async () => {
  cleanup()
  client.clear()
  actions.remove()
  vi.restoreAllMocks()
  await i18next.changeLanguage('en')
})

describe('分组本地参数校验与错误拦截设置', () => {
  it('支持一键勾选 Gemini 分组、配置状态码与上游 400/524 拦截项并完整保存', async () => {
    const user = userEvent.setup()
    mount()
    await screen.findByRole('checkbox', { name: 'gemini' })
    await user.click(
      screen.getByRole('button', { name: 'Select Gemini groups' })
    )
    await user.click(
      screen.getByRole('switch', { name: 'Enable local parameter validation' })
    )
    fireEvent.change(
      screen.getByRole('spinbutton', {
        name: 'Local error HTTP status — gemini',
      }),
      { target: { value: '422' } }
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Search groups' }),
      'GEMINI.PRO'
    )
    expect(
      screen.queryByRole('checkbox', { name: 'default' })
    ).not.toBeInTheDocument()
    fireEvent.change(
      screen.getByRole('spinbutton', {
        name: 'Local error HTTP status — gemini.pro',
      }),
      { target: { value: '400' } }
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalled())
    const saved = JSON.parse(
      (vi.mocked(api.put).mock.calls[0][1] as { value: string }).value
    )
    expect(saved.enabled).toBe(true)
    expect(saved.check_structure).toBe(true)
    expect(saved.check_media).toBe(true)
    expect(saved.check_generation_config).toBe(true)
    expect(saved.upstream_cooldown_seconds).toBe(60)
    expect(saved.upstream_status_codes).toBe('400,524')
    expect(saved.upstream_error_keywords).toBe('status_code=524,openai_error')
    expect(saved.rules).toEqual(
      expect.arrayContaining([
        { group: 'gemini', enabled: true, status_code: 422 },
        { group: 'gemini.pro', enabled: true, status_code: 400 },
        { group: 'default', enabled: false, status_code: 400 },
      ])
    )
  })

  it.each(['200', '600', '422.5'])(
    '状态码 %s 不属于可返回错误码时禁止保存',
    async (value) => {
      mount()
      const input = await screen.findByRole('spinbutton', {
        name: 'Local error HTTP status — gemini',
      })
      fireEvent.change(input, { target: { value } })
      fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
      expect(
        await screen.findByText('Enter an integer HTTP status from 400 to 599.')
      ).toBeVisible()
      expect(api.put).not.toHaveBeenCalled()
    }
  )

  it('开启但未选择分组时提示错误', async () => {
    const user = userEvent.setup()
    mount()
    await screen.findByRole('checkbox', { name: 'gemini' })
    await user.click(screen.getByRole('switch'))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(await screen.findByText('Select at least one group.')).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  })

  it('后台刷新保留尚未保存的状态码草稿', async () => {
    const rerender = mount()
    const input = await screen.findByRole('spinbutton', {
      name: 'Local error HTTP status — gemini',
    })
    fireEvent.change(input, { target: { value: '409' } })
    rerender(
      '{"enabled":false,"rules":[{"group":"gemini","enabled":false,"status_code":400}]}'
    )
    await waitFor(() =>
      expect(
        screen.getByRole('spinbutton', {
          name: 'Local error HTTP status — gemini',
        })
      ).toHaveValue(409)
    )
  })

  it('普通管理员不显示设置，也不读取分组', () => {
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, user: { id: 2, username: 'admin', role: 10 } },
    }))
    mount()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    expect(api.get).not.toHaveBeenCalled()
  })

  it('保存失败展示服务端原因，不提示保存成功', async () => {
    vi.mocked(api.put).mockResolvedValue({
      data: { success: false, message: '数据库暂不可用' },
    })
    mount()
    await screen.findByRole('checkbox', { name: 'gemini' })
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('数据库暂不可用')
    )
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('分组读取失败可以重试，恢复后支持键盘勾选', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('读取失败'))
    const user = userEvent.setup()
    mount()
    await user.click(await screen.findByRole('button', { name: 'Retry' }))
    const checkbox = await screen.findByRole('checkbox', { name: 'gemini' })
    checkbox.focus()
    await user.keyboard(' ')
    expect(checkbox).toBeChecked()
  })

  it('没有分组时展示空列表，不能启用空策略', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: { success: true, data: [] },
    })
    const user = userEvent.setup()
    mount()
    expect(await screen.findByText('No data')).toBeVisible()
    await user.click(screen.getByRole('switch'))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(await screen.findByText('Select at least one group.')).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  })

  it('中文界面显示中文设置名称和状态码标签，加载分组不抢焦点', async () => {
    i18next.addResourceBundle('zh', 'translation', zh.translation)
    await i18next.changeLanguage('zh')
    mount()
    expect(
      screen.getByRole('switch', { name: '启用本地参数校验' })
    ).toBeVisible()
    const input = await screen.findByRole('spinbutton', {
      name: '本地返回状态码 — gemini',
    })
    expect(input).toBeVisible()
    expect(input).not.toHaveFocus()
    expect(screen.getByRole('textbox', { name: '搜索分组' })).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '保存更改' }))
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith('设置更新成功')
    )
  })
})
