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
const { QueryClient, QueryClientProvider, notifyManager, defaultScheduler } =
  await import('@tanstack/react-query')
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

const { RateLimitSection } = await import('../rate-limit-section')
const { SettingsPageProvider } =
  await import('../../components/settings-page-context')
const defaultValues = {
  AsyncMediaConcurrency: 4,
  ModelRequestRateLimitEnabled: false,
  ModelRequestRateLimitDurationMinutes: 1,
  ModelRequestRateLimitCount: 0,
  ModelRequestRateLimitSuccessCount: 1000,
  ModelRequestRateLimitGroup: '',
  ModelRequestRateLimitModel: '',
}

async function mount(role = 100) {
  useAuthStore.setState((state) => ({
    auth: { ...state.auth, user: { id: 31, username: 'ui-fixture', role } },
  }))
  const actions = document.createElement('div')
  document.body.append(actions)
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <SettingsPageProvider actionsContainer={actions}>
            <RateLimitSection defaultValues={defaultValues} />
          </SettingsPageProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
  )
  const field = container.querySelector<HTMLInputElement>(
    'input[name="AsyncMediaConcurrency"]'
  )
  assert.ok(field)
  return field
}

test('速率限制页默认显示四个媒体并发，关闭速率限制仍能单独设置并发和零上限', async () => {
  const field = await mount()
  assert.equal(field.value, '4')
  assert.equal(field.disabled, false)
  await fill(field, '8')
  await act(async () => button('Save rate limits').click())
  await flush()
  assert.ok(
    calls.some(
      (call) =>
        JSON.stringify(call.data) ===
        JSON.stringify({ key: 'AsyncMediaConcurrency', value: 8 })
    )
  )
  calls.length = 0
  await fill(field, '0')
  await act(async () => button('Save rate limits').click())
  await flush()
  assert.ok(
    calls.some(
      (call) =>
        JSON.stringify(call.data) ===
        JSON.stringify({ key: 'AsyncMediaConcurrency', value: 0 })
    )
  )
})

test('并发数小数和超过范围时禁止保存', async () => {
  await i18n.changeLanguage('zh')
  const field = await mount()
  for (const value of ['1.5', '-1', '257']) {
    await fill(field, value)
    await act(async () => button(i18n.t('Save rate limits')).click())
    await flush()
    assert.equal(calls.filter((call) => call.data).length, 0)
    assert.equal(field.getAttribute('aria-invalid'), 'true')
    assert.ok(container.textContent?.includes('请输入 0 至 256 的整数。'))
  }
})

test('普通管理员只能查看媒体并发数，中文标题和说明正确显示', async () => {
  await i18n.changeLanguage('zh')
  const field = await mount(10)
  assert.equal(field.disabled, true)
  assert.ok(container.textContent?.includes('后台媒体并发数'))
  assert.ok(container.textContent?.includes('原有请求速率限制仍生效'))
})
