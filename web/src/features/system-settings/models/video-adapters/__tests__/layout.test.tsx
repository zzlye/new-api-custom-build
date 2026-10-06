import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { VideoAdaptersSection, type AdapterRegistry } from '../index'

const clients: QueryClient[] = []
const originalAuth = useAuthStore.getState().auth

// 测试浏览器缺少动画查询接口，仅补齐浏览器边界，保留真实滚动组件。
const animationDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)
beforeAll(() => {
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})
afterAll(() => {
  if (animationDescriptor) {
    Object.defineProperty(
      Element.prototype,
      'getAnimations',
      animationDescriptor
    )
  } else {
    Reflect.deleteProperty(Element.prototype, 'getAnimations')
  }
})

afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.setState({ auth: originalAuth })
})

// 只替换接口边界，使用真实管理页面和滚动组件验证列表行为。
async function setup(models = ['sd2.0', 'sd-2.5-10-10-10', 'sd-2.5-30-10-10']) {
  const registry: AdapterRegistry = {
    version: 3,
    templates: [],
    rules: models.map((model) => ({
      id: model,
      enabled: true,
      channel_ids: [8],
      models: [model],
      template_id: '',
    })),
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        registry,
        channels: [{ id: 8, name: '视频渠道', models: models.join(',') }],
      },
    },
  })
  const put = vi.spyOn(api, 'put').mockImplementation(async (_url, data) => ({
    data: { success: true, data: { ...(data as AdapterRegistry), version: 4 } },
  }))
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 1, username: 'fixture', role: 100 } as NonNullable<
        typeof originalAuth.user
      >,
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <VideoAdaptersSection />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: 'Add row' })
  return { put, registry }
}

test('移除配置说明，规则使用固定高度且适应小屏幕的内部滚动区', async () => {
  await setup()
  expect(
    screen.queryByText('How to configure video adapters')
  ).not.toBeInTheDocument()
  const region = screen.getByRole('region', { name: 'Video adapters' })
  expect(region).toHaveClass('h-[min(36rem,60dvh)]', 'w-full', 'min-w-0')
  expect(
    region.querySelector('[data-slot=scroll-area-viewport]')
  ).not.toBeNull()
  expect(within(region).getAllByRole('article')).toHaveLength(3)
  expect(region).not.toContainElement(
    screen.getByRole('button', { name: 'Add row' })
  )
  expect(region).not.toContainElement(
    screen.getByRole('button', { name: 'Publish rules' })
  )
})

test('搜索结果为空或恢复后，列表高度和未匹配规则保持不变', async () => {
  await setup()
  const region = screen.getByRole('region', { name: 'Video adapters' })
  const search = screen.getByRole('textbox', { name: 'Search video adapters' })
  fireEvent.change(search, { target: { value: 'sd2.0' } })
  expect(within(region).getAllByRole('article')).toHaveLength(1)
  fireEvent.change(search, { target: { value: 'missing' } })
  expect(within(region).queryAllByRole('article')).toHaveLength(0)
  expect(
    within(region).getByText(
      'No matching adapter rules. Add a row to bind a template.'
    )
  ).toBeVisible()
  expect(region).toHaveClass('h-[min(36rem,60dvh)]')
  fireEvent.change(search, { target: { value: '' } })
  expect(within(region).getAllByRole('article')).toHaveLength(3)
  expect(screen.getByRole('button', { name: 'Publish rules' })).toBeDisabled()
})

test('空列表添加、复制和删除仍在列表内进行，发布保留原始规则字段', async () => {
  const { put } = await setup([])
  const region = screen.getByRole('region', { name: 'Video adapters' })
  expect(within(region).queryAllByRole('article')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Add row' }))
  fireEvent.click(within(region).getByRole('button', { name: 'Duplicate' }))
  expect(within(region).getAllByRole('article')).toHaveLength(2)
  const copy = within(region).getAllByRole('article')[1]
  expect(
    within(copy).getByRole('checkbox', { name: 'Enabled' })
  ).not.toBeChecked()
  fireEvent.click(within(copy).getByRole('button', { name: 'Delete' }))
  expect(within(region).getAllByRole('article')).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'Publish rules' }))
  await screen.findByText('Video adapter rules published')
  expect(put).toHaveBeenCalledWith('/api/video-adapters', {
    version: 3,
    templates: [],
    rules: [
      {
        id: expect.any(String),
        enabled: true,
        channel_ids: [],
        models: [],
        template_id: '',
      },
    ],
  })
})
