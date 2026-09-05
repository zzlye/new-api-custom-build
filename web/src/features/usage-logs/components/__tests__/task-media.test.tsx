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
import assert from 'node:assert/strict'
import { after, afterEach, beforeEach, describe, test } from 'node:test'

import { Window } from 'happy-dom'
import type { Root } from 'react-dom/client'

import type { TaskDetails, TaskLog } from '../../types'

// 为媒体展示和权限按钮创建独立页面，只模拟请求和浏览器文件地址这两个边界。
const dom = new Window({ url: 'http://localhost/' })
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'PointerEvent',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: dom[key],
  })
}
Object.defineProperty(globalThis, 'requestAnimationFrame', {
  configurable: true,
  value: dom.requestAnimationFrame.bind(dom),
})
Object.defineProperty(globalThis, 'cancelAnimationFrame', {
  configurable: true,
  value: dom.cancelAnimationFrame.bind(dom),
})
Object.defineProperty(globalThis, 'getComputedStyle', {
  configurable: true,
  value: dom.getComputedStyle.bind(dom),
})
;(
  globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }
).IS_REACT_ACT_ENVIRONMENT = true

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider } = await import('react-i18next')
const { TaskMediaPreview } = await import('../task-media-result')
const { useTaskLogsColumns } = await import('../columns/task-logs-columns')
const { useReactTable, getCoreRowModel, flexRender } =
  await import('@tanstack/react-table')
const { DeleteTaskLogButton } = await import('../delete-task-log-button')
const { api } = await import('@/lib/api')
type FixtureApi = {
  get: (
    url: string,
    options?: unknown
  ) => Promise<{ data: Blob | { success: boolean; data: TaskDetails } }>
  delete: (url: string) => Promise<{ data: { success: boolean } }>
}
const fixtureApi = api as unknown as FixtureApi
const originalGet = fixtureApi.get
const originalDelete = fixtureApi.delete
const originalCreateURL = URL.createObjectURL
const originalRevokeURL = URL.revokeObjectURL
let getCalls: string[] = []
let deleteCalls: string[] = []
let released: string[] = []
let mediaLoadFails = false
let taskDetails: TaskDetails
const { useAuthStore } = await import('@/stores/auth-store')
const i18n = createInstance()
await i18n.init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
  interpolation: { escapeValue: false },
})
let root: Root
let container: HTMLDivElement
let client: InstanceType<typeof QueryClient>

beforeEach(() => {
  getCalls = []
  deleteCalls = []
  released = []
  mediaLoadFails = false
  taskDetails = {
    task_id: 'async_fixture',
    model_name: 'gpt-image-2',
    request_method: 'POST',
    request_path: '/v1/images/edits',
    request_format: 'openai-image',
    status: 'succeeded',
    prompt: '保持人物，背景改成晴天',
    prompt_source: 'request',
    input_available: true,
    parameters: { size: '1280x720', seed: '9007199254740993' },
    references: [
      {
        url: '/api/task/async_fixture/reference/0',
        kind: 'image',
        content_type: 'image/png',
        name: '原图.png',
        role: 'reference',
      },
    ],
    media: [
      {
        url: '/api/task/async_fixture/media/0',
        kind: 'image',
        content_type: 'image/png',
      },
    ],
    media_expired: false,
    expires_at: Math.floor(Date.now() / 1000) + 7200,
    submit_time: 1788616029,
    start_time: 1788616029,
    response_time: 1788616079,
    finish_time: 1788616080,
    response_status_code: 200,
  }
  fixtureApi.get = async (url) => {
    getCalls.push(url)
    if (url.endsWith('/details')) {
      return { data: { success: true, data: taskDetails } }
    }
    if (mediaLoadFails) throw new Error('request failed')
    return { data: new Blob(['media']) }
  }
  fixtureApi.delete = async (url) => {
    deleteCalls.push(url)
    return { data: { success: true } }
  }
  URL.createObjectURL = () => 'blob:generated-media'
  URL.revokeObjectURL = (url) => {
    released.push(url)
  }
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  fixtureApi.get = originalGet
  fixtureApi.delete = originalDelete
  URL.createObjectURL = originalCreateURL
  URL.revokeObjectURL = originalRevokeURL
  useAuthStore.setState((state) => ({ auth: { ...state.auth, user: null } }))
})
after(() => dom.happyDOM.cancelAsync())

function findButton(
  label: string,
  scope: ParentNode = document
): HTMLButtonElement | undefined {
  return [...scope.querySelectorAll('button')].find(
    (button) => button.textContent?.trim() === label
  )
}
const completedLog = {
  id: 23,
  user_id: 1,
  task_id: 'async_fixture',
  platform: 'internal',
  action: 'IMAGE',
  channel_id: 1,
  submit_time: 1,
  status: 'SUCCESS',
}

async function renderDelete(role: number, status = 'SUCCESS') {
  await act(async () => {
    useAuthStore.setState((state) => ({
      auth: { ...state.auth, user: { id: 1, username: 'fixture', role } },
    }))
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <DeleteTaskLogButton log={{ ...completedLog, status }} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
}

// 使用真实的结果列验证日志展示，文件过期提示不应覆盖生成失败原因。
function TaskDetailsFixture(props: { log: TaskLog }) {
  const columns = useTaskLogsColumns(false)
  const table = useReactTable({
    data: [props.log],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'fail_reason')
  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : null
}

describe('任务生成结果', () => {
  test('任务详情按需加载接口、提示词、参考图、结果和耗时', async () => {
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <QueryClientProvider client={client}>
            <TaskDetailsFixture log={{ ...completedLog, is_async: true }} />
          </QueryClientProvider>
        </I18nextProvider>
      )
    )
    assert.equal(getCalls.length, 0)
    await act(async () => findButton('View task details')?.click())
    // 等待查询通知提交到 React，不使用任意睡眠时间模拟网络速度。
    await act(async () => {
      await new Promise<void>((resolve) => setImmediate(resolve))
    })
    assert.ok(getCalls.includes('/api/task/async_fixture/details'))
    assert.ok(document.body.textContent?.includes('POST /v1/images/edits'))
    assert.ok(document.body.textContent?.includes('保持人物，背景改成晴天'))
    assert.ok(document.body.textContent?.includes('9007199254740993'))
    assert.ok(document.body.textContent?.includes('50s'))
    assert.ok(document.querySelector('img[alt="Reference image 1"]'))
    assert.ok(document.querySelector('img[alt="Generated image 1"]'))
    assert.ok(getCalls.includes('/api/task/async_fixture/reference/0'))
  })
  test('过期任务仍可查看文字详情但不再加载参考图和生成图片', async () => {
    taskDetails.media_expired = true
    taskDetails.expires_at = 1
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <QueryClientProvider client={client}>
            <TaskDetailsFixture
              log={{ ...completedLog, is_async: true, media_expired: true }}
            />
          </QueryClientProvider>
        </I18nextProvider>
      )
    )
    await act(async () => findButton('View task details')?.click())
    await act(async () => {
      await new Promise<void>((resolve) => setImmediate(resolve))
    })
    assert.ok(document.body.textContent?.includes('保持人物，背景改成晴天'))
    assert.ok(
      document.body.textContent?.includes('Reference media have expired')
    )
    assert.deepEqual(getCalls, ['/api/task/async_fixture/details'])
    assert.equal(document.querySelectorAll('img').length, 0)
  })
  test('失败任务超过文件保存期限后仍展示失败原因', async () => {
    const failure = 'upstream generation failed'
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskDetailsFixture
            log={{
              ...completedLog,
              status: 'FAILURE',
              is_async: true,
              media_expired: true,
              fail_reason: failure,
            }}
          />
        </I18nextProvider>
      )
    )
    const detailsButton = findButton(failure)
    assert.ok(detailsButton)
    assert.equal(detailsButton.title, 'Click to view full error message')
  })
  test('加载图片成功时展示生成图片且关闭后释放文件地址', async () => {
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskMediaPreview
            media={{
              url: '/api/task/async_fixture/media/0',
              kind: 'image',
              content_type: 'image/png',
            }}
            index={0}
          />
        </I18nextProvider>
      )
    )
    const image = document.querySelector('img[alt="Generated image 1"]')
    assert.equal(image?.getAttribute('src'), 'blob:generated-media')
    assert.equal(getCalls[0], '/api/task/async_fixture/media/0')
    await act(async () => root.render(null))
    assert.equal(released[0], 'blob:generated-media')
  })
  test('加载视频成功时展示带播放控件的视频', async () => {
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskMediaPreview
            media={{
              url: '/api/task/async_fixture/media/0',
              kind: 'video',
              content_type: 'video/mp4',
            }}
            index={0}
          />
        </I18nextProvider>
      )
    )
    const video = document.querySelector('video[aria-label="Generated video"]')
    assert.equal(video?.getAttribute('src'), 'blob:generated-media')
    assert.equal(video?.hasAttribute('controls'), true)
  })
  test('文件已经过期时展示过期提示且不请求文件', async () => {
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskMediaPreview
            media={{
              url: '/api/task/async_fixture/media/0',
              kind: 'image',
              content_type: 'image/png',
            }}
            expiresAt={1}
            index={0}
          />
        </I18nextProvider>
      )
    )
    assert.match(
      document.querySelector('[role="alert"]')?.textContent || '',
      /Generated files have expired/
    )
    assert.equal(getCalls.length, 0)
  })
  test('延长尚未清理的保存期限时已打开的预览恢复展示', async () => {
    const media = {
      url: '/api/task/async_fixture/media/0',
      kind: 'image' as const,
      content_type: 'image/png',
    }
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskMediaPreview media={media} expiresAt={1} index={0} />
        </I18nextProvider>
      )
    )
    assert.ok(document.querySelector('[role="alert"]'))
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskMediaPreview
            media={media}
            expiresAt={Math.floor(Date.now() / 1000) + 3600}
            index={0}
          />
        </I18nextProvider>
      )
    )
    assert.equal(document.querySelector('[role="alert"]'), null)
    assert.equal(
      document
        .querySelector('img[alt="Generated image 1"]')
        ?.getAttribute('src'),
      'blob:generated-media'
    )
    assert.deepEqual(getCalls, [media.url])
  })
  test('文件请求失败时显示加载失败提示', async () => {
    mediaLoadFails = true
    await act(async () =>
      root.render(
        <I18nextProvider i18n={i18n}>
          <TaskMediaPreview
            media={{
              url: '/api/task/async_fixture/media/0',
              kind: 'image',
              content_type: 'image/png',
            }}
            index={0}
          />
        </I18nextProvider>
      )
    )
    assert.match(
      document.querySelector('[role="alert"]')?.textContent || '',
      /Failed to load generated media/
    )
  })
})

describe('任务日志删除权限', () => {
  test('普通用户和普通管理员都看不到删除入口', async () => {
    await renderDelete(1)
    assert.equal(findButton('Delete'), undefined)
    await renderDelete(10)
    assert.equal(findButton('Delete'), undefined)
  })
  test('根用户面对运行中的任务时删除按钮禁用', async () => {
    await renderDelete(100, 'IN_PROGRESS')
    assert.equal(findButton('Delete')?.disabled, true)
  })
  test('根用户确认删除之前不发送删除请求', async () => {
    await renderDelete(100)
    await act(async () => findButton('Delete')?.click())
    assert.equal(deleteCalls.length, 0)
    assert.ok(document.querySelector('[role="alertdialog"]'))
    await act(async () => findButton('Cancel')?.click())
    assert.equal(deleteCalls.length, 0)
  })
  test('根用户确认后只删除选定任务日志', async () => {
    await renderDelete(100)
    await act(async () => findButton('Delete')?.click())
    const dialog = document.querySelector('[role="alertdialog"]')
    assert.ok(dialog)
    await act(async () => findButton('Delete', dialog)?.click())
    assert.equal(deleteCalls.length, 1)
    assert.equal(deleteCalls[0], '/api/task/23')
  })
})
