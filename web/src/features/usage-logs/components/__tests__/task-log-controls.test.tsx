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
import { after, afterEach, beforeEach, test } from 'node:test'

import { Window } from 'happy-dom'
import type { Root } from 'react-dom/client'

// 仅替换浏览器与请求边界，页面、路由、查询和表单均使用真实实现。
const dom = new Window({ url: 'http://localhost/' })
dom.document.write('<!doctype html><html><body></body></html>')
for (const key of [
  'window',
  'document',
  'navigator',
  'customElements',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'KeyboardEvent',
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
for (const key of [
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
  'matchMedia',
  'scrollTo',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: dom[key].bind(dom),
  })
}
;(
  globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }
).IS_REACT_ACT_ENVIRONMENT = true
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const {
  QueryClient,
  QueryClientProvider,
  notifyManager,
  defaultScheduler,
  focusManager,
  onlineManager,
} = await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider } = await import('react-i18next')
const { useAuthStore } = await import('@/stores/auth-store')
const { api } = await import('@/lib/api')
const en = (await import('@/i18n/locales/en.json')).default
const zh = (await import('@/i18n/locales/zh.json')).default
const i18n = createInstance()
await i18n.init({ lng: 'en', fallbackLng: 'en', resources: { en, zh } })
let container: HTMLDivElement
let root: Root
let client: InstanceType<typeof QueryClient>
const originalGet = api.get
const originalPut = api.put
const calls: { url: string; data?: unknown }[] = []
const boundary = api as unknown as {
  get: (url: string) => Promise<unknown>
  put: (url: string, data: unknown) => Promise<unknown>
}

beforeEach(async () => {
  notifyManager.setScheduler(queueMicrotask)
  await i18n.changeLanguage('en')
  dom.happyDOM.setWindowSize({ width: 1440, height: 900 })
  calls.length = 0
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  boundary.get = async (url) => {
    calls.push({ url })
    return {
      data: {
        success: true,
        data: { items: [], total: 300, page: 1, page_size: 100 },
      },
    }
  }
  boundary.put = async (url, data) => {
    calls.push({ url, data })
    return { data: { success: true } }
  }
  useAuthStore.setState((state) => ({
    auth: {
      ...state.auth,
      user: { id: 31, username: 'ui-fixture', role: 100 },
    },
  }))
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  api.get = originalGet
  api.put = originalPut
  notifyManager.setScheduler(defaultScheduler)
  useAuthStore.setState((state) => ({ auth: { ...state.auth, user: null } }))
  document.body.innerHTML = ''
})
after(() => dom.happyDOM.cancelAsync())

function button(label: string) {
  const result = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((element) => element.textContent?.trim() === label)
  assert.ok(result, `找不到按钮：${label}`)
  return result
}
async function fill(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    dom.HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(setter)
  await act(async () => {
    setter.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function flush() {
  await act(async () => {
    await new Promise<void>((resolve) => setImmediate(resolve))
  })
}

const {
  createRootRoute,
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
  Outlet,
} = await import('@tanstack/react-router')
const { UsageLogsTable } = await import('../usage-logs-table')
const { UsageLogsProvider } = await import('../usage-logs-provider')

async function mount(search = '') {
  const parent = createRootRoute({ component: Outlet })
  const authenticated = createRoute({
    getParentRoute: () => parent,
    id: '_authenticated',
    component: Outlet,
  })
  const route = createRoute({
    getParentRoute: () => authenticated,
    path: 'usage-logs/$section',
    validateSearch: (values: Record<string, unknown>) => values,
    component: () => (
      <UsageLogsProvider>
        <UsageLogsTable logCategory='task' />
      </UsageLogsProvider>
    ),
  })
  const router = createRouter({
    routeTree: parent.addChildren([authenticated.addChildren([route])]),
    history: createMemoryHistory({
      initialEntries: [`/usage-logs/task${search}`],
    }),
  })
  await act(async () => {
    await router.load()
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
  await flush()
  return router
}

test('模型筛选恢复地址状态，输入不立即请求，搜索回到第一页且重置清空', async () => {
  const router = await mount('?model=gpt-image-2&page=2')
  const model = document.querySelector<HTMLInputElement>(
    'input[aria-label="Model"]'
  )
  assert.ok(model)
  assert.equal(model.value, 'gpt-image-2')
  const before = calls.length
  await fill(model, ' gpt-image-2-4k ')
  assert.equal(calls.length, before)
  await act(async () => button('Search').click())
  await flush()
  assert.equal(router.state.location.search.model, 'gpt-image-2-4k')
  assert.equal(router.state.location.search.page, 1)
  assert.ok(
    calls.some(
      (call) =>
        new URL(call.url, 'http://localhost').searchParams.get('model_name') ===
        'gpt-image-2-4k'
    )
  )
  await act(async () => button('Reset').click())
  await flush()
  assert.equal(router.state.location.search.model, undefined)
  assert.equal(model.value, '')
})

test('任务列表不定时或在窗口重获焦点及重连后请求，手动搜索仍生效', async () => {
  const originalInterval = globalThis.setInterval
  const originalClearInterval = globalThis.clearInterval
  const intervals = new Map<number, () => void>()
  let sequence = 0
  globalThis.setInterval = ((callback: () => void) => {
    intervals.set(++sequence, callback)
    return sequence
  }) as unknown as typeof setInterval
  globalThis.clearInterval = ((id: number) =>
    intervals.delete(id)) as unknown as typeof clearInterval
  try {
    await mount()
    const before = calls.length
    await act(async () => {
      // 只推进当前轮次，回调新增的定时器留到下一轮，避免误触发无界循环。
      const scheduledCallbacks = [...intervals.values()]
      for (const callback of scheduledCallbacks) callback()
    })
    await act(async () => {
      focusManager.setFocused(false)
      focusManager.setFocused(true)
    })
    await act(async () => {
      onlineManager.setOnline(false)
      onlineManager.setOnline(true)
    })
    await flush()
    assert.equal(calls.length, before)
    await act(async () => button('Search').click())
    await flush()
    assert.ok(calls.length > before)
  } finally {
    await act(async () => root.render(null))
    globalThis.setInterval = originalInterval
    globalThis.clearInterval = originalClearInterval
    focusManager.setFocused(undefined)
    onlineManager.setOnline(true)
  }
})

test('窄屏筛选抽屉提供模型输入并提交相同的模型条件', async () => {
  dom.happyDOM.setWindowSize({ width: 390, height: 844 })
  const router = await mount()
  const trigger = button('Filter')
  assert.ok(trigger)
  await act(async () => trigger.click())
  const field = document.querySelector<HTMLInputElement>(
    'input[aria-label="Model"]'
  )
  assert.ok(field)
  await fill(field, 'gpt-image-2')
  const dialog = document.querySelector('[role="dialog"]')
  assert.ok(dialog)
  const search = [...dialog.querySelectorAll<HTMLButtonElement>('button')].find(
    (element) => element.textContent?.trim() === 'Search'
  )
  assert.ok(search)
  await act(async () => search.click())
  await flush()
  assert.equal(router.state.location.search.model, 'gpt-image-2')
})
