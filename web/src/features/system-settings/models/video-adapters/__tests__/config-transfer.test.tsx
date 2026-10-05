import { readFileSync } from 'node:fs'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

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
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  vi.unstubAllGlobals()
})

function setup(registry = base) {
  const onImport = vi.fn()
  const onOpenChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const props = {
    registry,
    channels: [{ id: 9, name: '测试渠道' }],
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
  complete({ data: { success: true, data: merged } })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add to draft' })).toBeEnabled()
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
