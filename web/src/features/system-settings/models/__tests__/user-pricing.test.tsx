import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  test,
  vi,
} from 'vitest'

import type {
  ModelPricingChange,
  ModelPricingConfig,
} from '@/features/model-pricing/api'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { UserPricingSettings } from '../user-pricing-settings'

const clients: QueryClient[] = []
const animationDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)
beforeAll(() =>
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
)
afterAll(() => {
  if (animationDescriptor) {
    Object.defineProperty(
      Element.prototype,
      'getAnimations',
      animationDescriptor
    )
  } else Reflect.deleteProperty(Element.prototype, 'getAnimations')
})
beforeEach(() => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})

function setup(
  options: { empty?: boolean; failSave?: boolean; failLoad?: boolean } = {}
) {
  let snapshot: ModelPricingConfig = {
    entries: options.empty
      ? []
      : [
          {
            model_name: 'image-test',
            version: 'user-v1',
            configured: { ModelPrice: 0.08 },
            effective: { ModelPrice: 0.08 },
          },
        ],
    options: {} as ModelPricingConfig['options'],
    empty_version: 'empty',
  }
  const global: ModelPricingConfig = {
    ...snapshot,
    entries: [
      {
        model_name: 'image-test',
        version: 'global-v1',
        configured: { ModelPrice: 0.1 },
        effective: { ModelPrice: 0.1 },
      },
    ],
  }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url.startsWith('/api/user/search')) {
      const term =
        new URL(url, 'http://localhost').searchParams.get('keyword') ?? ''
      const items = [
        { id: 81, username: 'alice', display_name: 'Alice' },
        { id: 82, username: 'bob', display_name: 'Bob' },
      ].filter((user) =>
        `${user.id} ${user.username}`.includes(term.toLowerCase())
      )
      return { data: { success: true, data: { items, total: items.length } } }
    }
    if (url === '/api/option/model_pricing') {
      return { data: { success: true, data: global } }
    }
    if (url.startsWith('/api/option/user_model_pricing/')) {
      if (options.failLoad) throw new Error('Pricing unavailable')
      return { data: { success: true, data: snapshot } }
    }
    if (url === '/api/status') return { data: { success: true, data: {} } }
    if (url === '/api/pricing') {
      return {
        data: { success: true, data: [], group_ratio: { default: 0.4 } },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
  const patch = vi
    .spyOn(api, 'patch')
    .mockImplementation(async (_url, body) => {
      if (options.failSave) throw new Error('Pricing changed; reload')
      const change = (body as { changes: ModelPricingChange[] }).changes[0]
      snapshot = {
        ...snapshot,
        entries: change.reset
          ? []
          : [
              {
                model_name: change.model_name,
                version: 'user-v2',
                configured: change.pricing,
                effective: change.pricing,
              },
            ],
      }
      return { data: { success: true } }
    })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <UserPricingSettings />
    </QueryClientProvider>
  )
  return { user: userEvent.setup(), patch }
}

test('搜索用户名称或编号，只显示匹配用户并保留已选用户', async () => {
  const { user } = setup()
  await user.click(
    await screen.findByRole('button', { name: '#81 · alice · Alice' })
  )
  const search = screen.getByRole('textbox', {
    name: 'Search users by name or ID',
  })
  await user.type(search, '82')
  await waitFor(() =>
    expect(
      screen.queryByRole('button', { name: '#81 · alice · Alice' })
    ).not.toBeInTheDocument()
  )
  expect(screen.getByText('Selected user: alice (#81)')).toBeVisible()
  expect(
    await screen.findByRole('button', { name: '#82 · bob · Bob' })
  ).toBeVisible()
  await user.clear(search)
  await user.type(search, 'nobody')
  expect(await screen.findByText('No users found')).toBeVisible()
})

test('用户未定价时可以选择全局模型并保存零价，保存不修改全局接口', async () => {
  const { user, patch } = setup({ empty: true })
  await user.click(
    await screen.findByRole('button', { name: '#81 · alice · Alice' })
  )
  expect(await screen.findByText('No user prices configured')).toBeVisible()
  const model = screen.getByRole('combobox', { name: 'Select model' })
  await user.click(model)
  await user.type(model, 'image-test')
  await user.click(await screen.findByRole('option', { name: 'image-test' }))
  const price = await screen.findByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() =>
    expect(patch).toHaveBeenCalledWith('/api/option/user_model_pricing/81', {
      changes: [
        expect.objectContaining({
          model_name: 'image-test',
          expected_version: 'empty',
          pricing: { ModelPrice: 0, 'billing_setting.billing_mode': 'ratio' },
        }),
      ],
    })
  )
  expect(await screen.findByText('User price configured')).toBeVisible()
})

test('恢复全局价格需要确认，取消保留规则，确认只删除所选用户模型', async () => {
  const { user, patch } = setup()
  await user.click(
    await screen.findByRole('button', { name: '#81 · alice · Alice' })
  )
  await user.click(await screen.findByRole('button', { name: 'image-test' }))
  await user.click(screen.getByRole('button', { name: 'Restore global price' }))
  await user.click(
    within(screen.getByRole('alertdialog')).getByRole('button', {
      name: 'Cancel',
    })
  )
  expect(patch).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Restore global price' }))
  await user.click(
    within(screen.getByRole('alertdialog')).getByRole('button', {
      name: 'Restore',
    })
  )
  await waitFor(() =>
    expect(patch).toHaveBeenCalledWith('/api/option/user_model_pricing/81', {
      changes: [
        {
          model_name: 'image-test',
          expected_version: 'user-v1',
          pricing: {},
          reset: true,
        },
      ],
    })
  )
  expect(await screen.findByText('Using global price')).toBeVisible()
})

test('修改价格后切换用户会提示丢弃，取消后价格保留', async () => {
  const { user } = setup()
  await user.click(
    await screen.findByRole('button', { name: '#81 · alice · Alice' })
  )
  await user.click(await screen.findByRole('button', { name: 'image-test' }))
  const price = await screen.findByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.06')
  await user.click(screen.getByRole('button', { name: '#82 · bob · Bob' }))
  const dialog = await screen.findByRole('alertdialog', {
    name: 'Discard unsaved changes?',
  })
  await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  expect(price).toHaveValue('0.06')
  expect(screen.getByText('Selected user: alice (#81)')).toBeVisible()
})

test('保存失败显示错误并保留用户草稿', async () => {
  const { user } = setup({ failSave: true })
  await user.click(
    await screen.findByRole('button', { name: '#81 · alice · Alice' })
  )
  await user.click(await screen.findByRole('button', { name: 'image-test' }))
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  expect(await screen.findByText('Pricing changed; reload')).toBeVisible()
  expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
    '0.08'
  )
})

test('读取用户价格失败展示错误，不显示可保存的空价格', async () => {
  const { user } = setup({ failLoad: true })
  await user.click(
    await screen.findByRole('button', { name: '#81 · alice · Alice' })
  )
  expect(await screen.findByText('Pricing unavailable')).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Save model prices' })
  ).not.toBeInTheDocument()
})
