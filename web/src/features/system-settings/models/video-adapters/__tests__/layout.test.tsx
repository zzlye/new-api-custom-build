import { readFileSync } from 'node:fs'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterAll, afterEach, beforeAll, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { VideoAdaptersSection, type AdapterRegistry } from '../index'

const clients: QueryClient[] = []
const originalAuth = useAuthStore.getState().auth
const importBundle = JSON.parse(
  readFileSync('../setting/video_setting/import-template.json', 'utf8')
) as {
  templates: AdapterRegistry['templates']
  rules: AdapterRegistry['rules']
}
const animationDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)

// 仅补齐浏览器动画边界，表格、分页、标签页和编辑弹窗均使用真实组件。
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

async function setup(
  models = ['sd2.0', 'sd-2.5-10-10-10', 'sd-2.5-30-10-10'],
  templates: AdapterRegistry['templates'] = []
) {
  const registry: AdapterRegistry = {
    version: 3,
    templates,
    rules: models.map((model, index) => ({
      id: model,
      enabled: true,
      channel_ids: [8 + index],
      models: [model],
      template_id: '',
    })),
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        registry,
        channels: models.map((model, index) => ({
          id: 8 + index,
          name: `视频渠道${index + 1}`,
          models: model,
        })),
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
  return { put, registry, user: userEvent.setup() }
}

test('规则默认以摘要表格展示，不同时展开全部编辑表单', async () => {
  await setup()
  const region = screen.getByRole('region', { name: 'Video adapters' })
  expect(region).toHaveClass('h-[min(36rem,60dvh)]', 'w-full', 'min-w-0')
  const table = within(region).getByRole('table')
  expect(within(table).getAllByRole('columnheader')).toHaveLength(5)
  expect(within(table).getAllByRole('row')).toHaveLength(4)
  expect(within(region).queryByRole('textbox')).not.toBeInTheDocument()
  expect(within(region).queryByRole('article')).not.toBeInTheDocument()
  expect(region).not.toContainElement(
    screen.getByRole('button', { name: 'Add row' })
  )
  expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
})

test('超过一页的规则通过页码切换，不渲染其他页的行', async () => {
  await setup(Array.from({ length: 12 }, (_, index) => `model-${index + 1}`))
  const region = screen.getByRole('region', { name: 'Video adapters' })
  expect(within(region).getAllByRole('row')).toHaveLength(11)
  expect(within(region).queryByText('model-11')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  expect(within(region).getAllByRole('row')).toHaveLength(3)
  expect(within(region).getByText('model-11')).toBeVisible()
  expect(within(region).queryByText('model-1')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Go to first page' }))
  expect(within(region).getByText('model-1')).toBeVisible()
})

test('修改每页行数后按新分页范围展示规则', async () => {
  const { user } = await setup(
    Array.from({ length: 25 }, (_, index) => `model-${index + 1}`)
  )
  await user.click(screen.getByRole('combobox'))
  await user.click(screen.getByRole('option', { name: '20' }))
  const region = screen.getByRole('region', { name: 'Video adapters' })
  expect(within(region).getAllByRole('row')).toHaveLength(21)
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  expect(within(region).getAllByRole('row')).toHaveLength(6)
  expect(within(region).getByText('model-21')).toBeVisible()
})

test('模型输入保留分隔符，按Esc关闭编辑时不丢失未失焦的草稿', async () => {
  const { user } = await setup(['model-a'])
  await user.click(screen.getByRole('button', { name: 'Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Edit' })
  const models = within(dialog).getByRole('textbox', {
    name: 'Upstream model names',
  })
  await user.clear(models)
  await user.type(models, 'new-model,\nsecond-model,')
  expect(models).toHaveValue('new-model,\nsecond-model,')
  await user.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(screen.getByText('new-model，second-model')).toBeVisible()
})

test('发布失败后保留未发布的修改，切换分页不会丢失原规则', async () => {
  const { put, user } = await setup(
    Array.from({ length: 12 }, (_, index) => `model-${index + 1}`)
  )
  put.mockResolvedValueOnce({
    data: { success: false, message: '发布失败示例' },
  })
  const row = screen.getByRole('row', { name: /model-1\s/ })
  await user.click(within(row).getByRole('switch', { name: 'Enabled' }))
  await user.click(screen.getByRole('button', { name: 'Publish rules' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('发布失败示例')
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  await user.click(screen.getByRole('button', { name: 'Go to first page' }))
  expect(
    within(screen.getByRole('row', { name: /model-1\s/ })).getByRole('switch', {
      name: 'Enabled',
    })
  ).not.toBeChecked()
  expect(screen.getByRole('button', { name: 'Publish rules' })).toBeEnabled()
})

test('后续页按渠道ID搜索时回到首页，空结果恢复后保留全部规则', async () => {
  await setup(Array.from({ length: 12 }, (_, index) => `model-${index + 1}`))
  const region = screen.getByRole('region', { name: 'Video adapters' })
  fireEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  const search = screen.getByRole('textbox', { name: 'Search video adapters' })
  fireEvent.change(search, { target: { value: '8' } })
  expect(within(region).getAllByRole('row')).toHaveLength(4)
  expect(within(region).getByText('model-1')).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Go to previous page' })
  ).toBeDisabled()
  fireEvent.change(search, { target: { value: 'missing' } })
  expect(
    within(region).getByText(
      'No matching adapter rules. Add a row to bind a template.'
    )
  ).toBeVisible()
  fireEvent.change(search, { target: { value: '' } })
  expect(within(region).getAllByRole('row')).toHaveLength(11)
  expect(screen.getByRole('button', { name: 'Publish rules' })).toBeDisabled()
})

test('规则、通用模板和批量关联通过标签页切换，并保留规则页码', async () => {
  const { user } = await setup(
    Array.from({ length: 12 }, (_, index) => `model-${index + 1}`)
  )
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  await user.click(
    screen.getByRole('tab', { name: 'Manage reusable templates' })
  )
  expect(
    screen.getByRole('tab', { name: 'Manage reusable templates' })
  ).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('combobox', { name: 'Edit template' })).toBeVisible()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  await user.click(
    screen.getByRole('tab', { name: 'Bind documented models in bulk' })
  )
  expect(
    screen.getByRole('listbox', { name: 'Channels for documented models' })
  ).toBeVisible()
  await user.click(screen.getByRole('tab', { name: 'Rules' }))
  expect(screen.getByText('model-11')).toBeVisible()
  expect(screen.queryByText('model-1')).not.toBeInTheDocument()
})

test('模板改名后切换标签页保留草稿，规则摘要同步显示且支持模板名称搜索', async () => {
  const { put, user } = await setup(
    ['model-a'],
    importBundle.templates.slice(0, 1)
  )
  await user.click(
    screen.getByRole('tab', { name: 'Manage reusable templates' })
  )
  fireEvent.change(screen.getByRole('combobox', { name: 'Edit template' }), {
    target: { value: importBundle.templates[0].id },
  })
  fireEvent.change(screen.getByRole('textbox', { name: 'Template name' }), {
    target: { value: 'Renamed Template' },
  })
  await user.click(screen.getByRole('tab', { name: 'Rules' }))
  await user.click(screen.getByRole('button', { name: 'Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Edit' })
  fireEvent.change(
    within(dialog).getByRole('combobox', { name: 'Protocol template' }),
    { target: { value: importBundle.templates[0].id } }
  )
  await user.click(within(dialog).getByRole('button', { name: 'Close' }))
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Search video adapters' }),
    { target: { value: ' renamed template ' } }
  )
  expect(screen.getByText('model-a')).toBeVisible()
  expect(screen.getByText('Renamed Template')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Publish rules' }))
  await screen.findByText('Video adapter rules published')
  expect((put.mock.calls[0][1] as AdapterRegistry).templates[0].name).toBe(
    'Renamed Template'
  )
})

test('批量关联标签页沿用文档规则，重复关联不会创建重复规则', async () => {
  const bulkTemplate = {
    ...importBundle.templates[0],
    id: 'sd2-5-720p',
  }
  const { put, user } = await setup(['model-a'], [bulkTemplate])
  await user.click(
    screen.getByRole('tab', { name: 'Bind documented models in bulk' })
  )
  const channels = screen.getByRole('listbox', {
    name: 'Channels for documented models',
  }) as HTMLSelectElement
  channels.options[0].selected = true
  fireEvent.change(channels)
  await user.click(
    screen.getByRole('button', { name: 'Add documented model rules' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Add documented model rules' })
  )
  await user.click(screen.getByRole('tab', { name: 'Rules' }))
  await user.click(screen.getByRole('button', { name: 'Publish rules' }))
  await screen.findByText('Video adapter rules published')
  const rules = (put.mock.calls[0][1] as AdapterRegistry).rules
  const documented = rules.filter((rule) => rule.template_id !== '')
  expect(documented.length).toBeGreaterThan(0)
  expect(new Set(documented.map((rule) => rule.template_id)).size).toBe(
    documented.length
  )
  expect(
    documented.every(
      (rule) => rule.channel_ids.length === 1 && rule.channel_ids[0] === 8
    )
  ).toBe(true)
  expect(rules[0].models).toEqual(['model-a'])
})

test('从其他标签页导入时回到规则首页，清空搜索但保留原规则', async () => {
  const { put, registry, user } = await setup(
    Array.from({ length: 12 }, (_, index) => `model-${index + 1}`)
  )
  const next: AdapterRegistry = {
    ...registry,
    rules: [
      ...registry.rules,
      { ...registry.rules[0], id: 'imported-rule', models: ['imported-model'] },
    ],
  }
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: next },
  })
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Search video adapters' }),
    { target: { value: 'missing' } }
  )
  await user.click(
    screen.getByRole('tab', { name: 'Manage reusable templates' })
  )
  await user.click(screen.getByRole('button', { name: 'Import configuration' }))
  const dialog = await screen.findByRole('dialog', {
    name: 'Import video adapter configuration',
  })
  fireEvent.change(
    within(dialog).getByRole('textbox', {
      name: 'Or paste configuration JSON',
    }),
    { target: { value: '{}' } }
  )
  await user.click(
    within(dialog).getByRole('button', { name: 'Validate and preview' })
  )
  await waitFor(() =>
    expect(
      within(dialog).getByRole('button', { name: 'Add to draft' })
    ).toBeEnabled()
  )
  await user.click(within(dialog).getByRole('button', { name: 'Add to draft' }))
  expect(screen.getByRole('tab', { name: 'Rules' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(
    screen.getByRole('textbox', { name: 'Search video adapters' })
  ).toHaveValue('')
  expect(
    screen.getByRole('button', { name: 'Go to previous page' })
  ).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Publish rules' }))
  await screen.findByText('Video adapter rules published')
  expect(put).toHaveBeenCalledWith('/api/video-adapters', next)
})

test('仅编辑选中规则，关闭弹窗并跨页后仍可发布完整草稿', async () => {
  const { put, registry, user } = await setup(
    Array.from({ length: 12 }, (_, index) => `model-${index + 1}`)
  )
  const row = screen.getByRole('row', { name: /model-1\s/ })
  await user.click(within(row).getByRole('button', { name: 'Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Edit' })
  expect(within(dialog).getAllByRole('article')).toHaveLength(1)
  const models = within(dialog).getByRole('textbox', {
    name: 'Upstream model names',
  })
  fireEvent.change(models, { target: { value: 'updated-model' } })
  fireEvent.blur(models)
  await user.click(within(dialog).getByRole('button', { name: 'Close' }))
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  await user.click(screen.getByRole('button', { name: 'Go to first page' }))
  expect(screen.getByText('updated-model')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Publish rules' }))
  await screen.findByText('Video adapter rules published')
  expect(put).toHaveBeenCalledWith('/api/video-adapters', {
    ...registry,
    rules: registry.rules.map((rule, index) =>
      index === 0 ? { ...rule, models: ['updated-model'] } : rule
    ),
  })
})

test('末页删除最后一条规则后回到有效页码，编辑不会重置当前页', async () => {
  const { user } = await setup(
    Array.from({ length: 11 }, (_, index) => `model-${index + 1}`)
  )
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  const row = screen.getByRole('row', { name: /model-11/ })
  await user.click(within(row).getByRole('switch', { name: 'Enabled' }))
  expect(screen.getByText('model-11')).toBeVisible()
  expect(within(row).getByRole('switch', { name: 'Enabled' })).not.toBeChecked()
  await user.click(within(row).getByRole('button', { name: 'Delete' }))
  await waitFor(() => expect(screen.getByText('model-1')).toBeVisible())
  expect(
    screen.getByRole('button', { name: 'Go to previous page' })
  ).toBeDisabled()
})

test('空列表添加和复制打开单条编辑，复制禁用且发布保留规则字段', async () => {
  const { put, user } = await setup([])
  await user.click(screen.getByRole('button', { name: 'Add row' }))
  let dialog = await screen.findByRole('dialog', { name: 'Edit' })
  await user.click(within(dialog).getByRole('button', { name: 'Close' }))
  await user.click(screen.getByRole('button', { name: 'Duplicate' }))
  dialog = await screen.findByRole('dialog', { name: 'Edit' })
  expect(
    within(dialog).getByRole('checkbox', { name: 'Enabled' })
  ).not.toBeChecked()
  await user.click(within(dialog).getByRole('button', { name: 'Close' }))
  const region = screen.getByRole('region', { name: 'Video adapters' })
  const copy = within(region).getAllByRole('row')[2]
  await user.click(within(copy).getByRole('button', { name: 'Delete' }))
  await user.click(screen.getByRole('button', { name: 'Publish rules' }))
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

test('添加规则会清除搜索并定位新规则所在页，长文本不扩大列宽', async () => {
  const { user } = await setup(
    Array.from(
      { length: 10 },
      (_, index) => `model-${index + 1}-${'long-name-'.repeat(30)}`
    )
  )
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Search video adapters' }),
    { target: { value: 'missing' } }
  )
  await user.click(screen.getByRole('button', { name: 'Add row' }))
  const dialog = await screen.findByRole('dialog', { name: 'Edit' })
  await user.click(within(dialog).getByRole('button', { name: 'Close' }))
  expect(
    screen.getByRole('textbox', { name: 'Search video adapters' })
  ).toHaveValue('')
  const region = screen.getByRole('region', { name: 'Video adapters' })
  expect(within(region).getAllByRole('row')).toHaveLength(2)
  expect(within(region).getByText('Channel default')).toBeVisible()
  expect(within(region).getByRole('table')).toHaveClass('table-fixed')
})
