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

import { AdapterConfigTransfer } from '../config-transfer'
import type { AdapterRegistry } from '../index'

// 使用随程序下载的真实模板，避免测试示例与用户拿到的文件不一致。
const bundle = JSON.parse(
  readFileSync('../setting/video_setting/import-template.json', 'utf8')
) as {
  format: string
  format_version: number
  templates: AdapterRegistry['templates']
  rules: AdapterRegistry['rules']
}
const base: AdapterRegistry = { version: 7, templates: [], rules: [] }
const merged: AdapterRegistry = {
  version: 7,
  templates: bundle.templates,
  rules: bundle.rules.map((rule) => ({ ...rule, channel_ids: [9] })),
}
const clients: QueryClient[] = []
const channels = [
  { id: 9, name: '测试渠道' },
  { id: 129, name: 'Seedance 视频' },
  { id: 31, name: '备用视频' },
]

// 补齐测试环境的动画查询接口，保留真实滚动组件验证列表布局。
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
  vi.unstubAllGlobals()
})

function setup(
  registry = base,
  availableChannels = [{ id: 9, name: '测试渠道' }]
) {
  const onImport = vi.fn()
  const onOpenChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const props = {
    registry,
    channels: availableChannels,
    onImport,
    onOpenChange,
  }
  const view = render(
    <QueryClientProvider client={client}>
      <AdapterConfigTransfer {...props} />
    </QueryClientProvider>
  )
  return {
    ...view,
    onImport,
    onOpenChange,
    client,
    props,
    user: userEvent.setup(),
  }
}

async function openImport(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Import configuration' }))
  expect(
    await screen.findByRole('dialog', {
      name: 'Import video adapter configuration',
    })
  ).toBeVisible()
}

test('校验后加入草稿，未点发布不会保存线上配置', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: merged } })
  const put = vi.spyOn(api, 'put')
  const { user, onImport, onOpenChange } = setup()
  await openImport(user)
  expect(
    screen.getByRole('button', { name: 'Validate and preview' })
  ).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Add to draft' })).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await user.click(screen.getByRole('checkbox', { name: '测试渠道 · 9' }))
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
  )
  expect(post).toHaveBeenCalledWith('/api/video-adapters/import-preview', {
    registry: base,
    bundle,
    channel_ids: [9],
  })
  expect(screen.getByLabelText('Import preview')).toHaveTextContent(
    'sd-2.5-ch1'
  )
  expect(onImport).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Add to draft' }))
  expect(onImport).toHaveBeenCalledWith(merged)
  expect(onOpenChange).toHaveBeenLastCalledWith(false)
  expect(put).not.toHaveBeenCalled()
})

test('无效 JSON 不发请求，服务端字段错误显示原始原因且不能导入', async () => {
  const post = vi.spyOn(api, 'post').mockRejectedValue({
    response: {
      data: { success: false, message: '未知统一输入字段 image_refs' },
    },
  })
  const { user, onImport } = setup()
  await openImport(user)
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: '{bad json' },
  })
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Enter valid JSON')
  expect(post).not.toHaveBeenCalled()
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    '未知统一输入字段 image_refs'
  )
  expect(screen.getByRole('button', { name: 'Add to draft' })).toBeDisabled()
  expect(onImport).not.toHaveBeenCalled()
})

test('上传 UTF-8 文件保留 false 和零，取消不改变草稿', async () => {
  const { user, onImport, onOpenChange } = setup()
  await openImport(user)
  const json = JSON.stringify({ ...bundle, sample: { audio: false, seed: 0 } })
  const file = new File([`\uFEFF${json}`], '配置.json', {
    type: 'application/json',
  })
  Object.defineProperty(file, 'text', { value: async () => `\uFEFF${json}` })
  await user.upload(screen.getByLabelText('Configuration JSON file'), file)
  await waitFor(() =>
    expect(screen.getByLabelText('Or paste configuration JSON')).toHaveValue(
      json
    )
  )
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(onImport).not.toHaveBeenCalled()
  expect(onOpenChange).toHaveBeenLastCalledWith(false)
})

test('超大文件与读取失败不清空已输入配置', async () => {
  const { user } = setup()
  await openImport(user)
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: 'existing input' },
  })
  const large = new File(['x'], 'large.json', { type: 'application/json' })
  Object.defineProperty(large, 'size', { value: 2 * 1024 * 1024 + 1 })
  await user.upload(screen.getByLabelText('Configuration JSON file'), large)
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'must not exceed 2 MiB'
  )
  const broken = new File(['x'], 'broken.json', { type: 'application/json' })
  Object.defineProperty(broken, 'text', {
    value: async () => {
      throw new Error('read failed')
    },
  })
  await user.upload(screen.getByLabelText('Configuration JSON file'), broken)
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not read')
  expect(screen.getByLabelText('Or paste configuration JSON')).toHaveValue(
    'existing input'
  )
})

test('修改输入或当前草稿会使旧预览失效', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: merged },
  })
  const view = setup()
  await openImport(view.user)
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await view.user.click(
    screen.getByRole('button', { name: 'Validate and preview' })
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
  )
  view.rerender(
    <QueryClientProvider client={view.client}>
      <AdapterConfigTransfer
        {...view.props}
        registry={{ ...base, version: 8 }}
      />
    </QueryClientProvider>
  )
  expect(screen.getByRole('button', { name: 'Add to draft' })).toBeDisabled()
  expect(screen.getByRole('status')).toHaveTextContent('draft has changed')
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: '{}' },
  })
  expect(screen.queryByLabelText('Import preview')).not.toBeInTheDocument()
})

test('查询未结束时禁用重复校验及加入草稿，完成后恢复', async () => {
  let complete!: (value: unknown) => void
  vi.spyOn(api, 'post').mockImplementation(
    () =>
      new Promise((resolve) => {
        complete = resolve
      })
  )
  const { user } = setup()
  await openImport(user)
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  expect(
    await screen.findByRole('button', { name: 'Validating...' })
  ).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Add to draft' })).toBeDisabled()
  expect(screen.getByLabelText('Or paste configuration JSON')).toBeDisabled()
  expect(screen.getByRole('searchbox')).toBeDisabled()
  const checkbox = screen.getByRole('checkbox', { name: '测试渠道 · 9' })
  expect(checkbox).toHaveAttribute('aria-disabled', 'true')
  await user.click(checkbox)
  expect(checkbox).not.toBeChecked()
  complete({ data: { success: true, data: merged } })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
  )
  expect(screen.getByRole('searchbox')).toBeEnabled()
})

test('渠道名称与编号支持部分匹配，忽略大小写及首尾空格', async () => {
  const { user } = setup(base, channels)
  await openImport(user)
  const search = screen.getByRole('searchbox', {
    name: 'Search channels by name or ID',
  })
  await user.type(search, '  sEEd  ')
  expect(screen.getAllByRole('checkbox')).toHaveLength(1)
  expect(
    screen.getByRole('checkbox', { name: 'Seedance 视频 · 129' })
  ).toBeVisible()
  await user.clear(search)
  await user.type(search, '视频')
  expect(screen.getAllByRole('checkbox')).toHaveLength(2)
  await user.clear(search)
  await user.type(search, '9')
  expect(screen.getAllByRole('checkbox')).toHaveLength(2)
  expect(
    screen.queryByRole('checkbox', { name: '备用视频 · 31' })
  ).not.toBeInTheDocument()
  await user.clear(search)
  expect(screen.getAllByRole('checkbox')).toHaveLength(3)
})

test('筛选隐藏已选渠道不会丢失绑定，搜索不影响已校验的导入预览', async () => {
  const imported = {
    ...merged,
    rules: merged.rules.map((rule) => ({ ...rule, channel_ids: [9, 31] })),
  }
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: imported } })
  const { user, onImport } = setup(base, channels)
  await openImport(user)
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await user.click(screen.getByRole('checkbox', { name: '测试渠道 · 9' }))
  const search = screen.getByRole('searchbox')
  await user.type(search, '31')
  await user.click(screen.getByRole('checkbox', { name: '备用视频 · 31' }))
  expect(screen.getByText('Selected 2')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
  )
  expect(post).toHaveBeenCalledWith('/api/video-adapters/import-preview', {
    registry: base,
    bundle,
    channel_ids: [9, 31],
  })
  await user.clear(search)
  expect(screen.getByRole('checkbox', { name: '测试渠道 · 9' })).toBeChecked()
  expect(screen.getByRole('checkbox', { name: '备用视频 · 31' })).toBeChecked()
  expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
  await user.click(screen.getByRole('button', { name: 'Add to draft' }))
  expect(onImport).toHaveBeenCalledWith(imported)
})

test('筛选后取消已选渠道会使导入预览失效', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: merged } })
  const { user } = setup(base, channels)
  await openImport(user)
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await user.click(screen.getByRole('checkbox', { name: '测试渠道 · 9' }))
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
  )
  await user.type(screen.getByRole('searchbox'), '测试')
  await user.click(screen.getByRole('checkbox', { name: '测试渠道 · 9' }))
  expect(screen.getByRole('button', { name: 'Add to draft' })).toBeDisabled()
  expect(screen.queryByLabelText('Import preview')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  await waitFor(() =>
    expect(post).toHaveBeenLastCalledWith(
      '/api/video-adapters/import-preview',
      { registry: base, bundle, channel_ids: [] }
    )
  )
})

test('搜索无匹配时显示空状态，重新打开清空搜索但保留已选渠道', async () => {
  const { user } = setup(base, channels)
  await openImport(user)
  await user.click(screen.getByRole('checkbox', { name: '测试渠道 · 9' }))
  await user.type(screen.getByRole('searchbox'), '不存在')
  expect(screen.getByText('No channels found')).toBeVisible()
  expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
  expect(screen.getByText('Selected 1')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  await openImport(user)
  expect(screen.getByRole('searchbox')).toHaveValue('')
  expect(screen.getAllByRole('checkbox')).toHaveLength(3)
  expect(screen.getByRole('checkbox', { name: '测试渠道 · 9' })).toBeChecked()
})

test('渠道使用固定高度单列滚动列表，搜索框不随列表滚动', async () => {
  const { user } = setup(base, channels)
  await openImport(user)
  const region = screen.getByRole('region', {
    name: 'Bind imported rules to channels',
  })
  expect(region).toHaveClass('h-48', 'w-full', 'min-w-0')
  expect(
    region.querySelector('[data-slot=scroll-area-viewport]')
  ).not.toBeNull()
  expect(within(region).getAllByRole('listitem')).toHaveLength(3)
  expect(within(region).getByRole('list')).toHaveClass('flex', 'flex-col')
  expect(region).not.toContainElement(screen.getByRole('searchbox'))
})

test('没有渠道时保留原有提示，仍可导入文件中的渠道绑定', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: merged } })
  const { user } = setup(base, [])
  await openImport(user)
  expect(
    screen.getByText(
      'Create a channel before binding model rules. Templates can be imported without rules.'
    )
  ).toBeVisible()
  expect(screen.queryByText('No channels found')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Or paste configuration JSON'), {
    target: { value: JSON.stringify(bundle) },
  })
  await user.click(screen.getByRole('button', { name: 'Validate and preview' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/video-adapters/import-preview', {
      registry: base,
      bundle,
      channel_ids: [],
    })
  )
})

test('下载模板与说明，导出当前草稿不包含线上发布版本', async () => {
  let currentBlob: Blob
  const downloads: { name: string; blob: Blob }[] = []
  vi.stubGlobal(
    'URL',
    Object.assign(class extends URL {}, {
      createObjectURL: vi.fn((blob: Blob) => {
        currentBlob = blob
        return 'blob:video-config'
      }),
      revokeObjectURL: vi.fn(),
    })
  )
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
    function (this: HTMLAnchorElement) {
      downloads.push({ name: this.download, blob: currentBlob })
    }
  )
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: url.endsWith('import-template')
      ? JSON.stringify(bundle)
      : '# 填写说明',
  }))
  const { user } = setup(merged)
  await user.click(
    screen.getByRole('button', { name: 'Download configuration template' })
  )
  await waitFor(() => expect(downloads).toHaveLength(1))
  await user.click(
    screen.getByRole('button', { name: 'Download filling guide' })
  )
  await waitFor(() => expect(downloads).toHaveLength(2))
  await user.click(
    screen.getByRole('button', { name: 'Export current configuration' })
  )
  expect(downloads.map((item) => item.name)).toEqual([
    'video-adapters-template.json',
    'video-adapters-guide.md',
    'video-adapters-config.json',
  ])
  const exported = await new Promise<string>((resolve) => {
    const reader = new FileReader()
    reader.addEventListener('load', () => resolve(String(reader.result)), {
      once: true,
    })
    reader.readAsText(downloads[2].blob)
  })
  expect(JSON.parse(exported)).toEqual({
    format: 'newapi-video-adapters',
    format_version: 1,
    templates: merged.templates,
    rules: merged.rules,
  })
  await waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalled())
})
