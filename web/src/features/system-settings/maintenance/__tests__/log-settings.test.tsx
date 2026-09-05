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

// 只替换浏览器和请求边界，实际渲染清理页面、确认框及保存表单。
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
const { LogSettingsSection } = await import('../log-settings-section')
const { SettingsPageProvider } =
  await import('../../components/settings-page-context')
const { useAuthStore } = await import('@/stores/auth-store')
const { api } = await import('@/lib/api')
const en = (await import('@/i18n/locales/en.json')).default
const zh = (await import('@/i18n/locales/zh.json')).default
const i18n = createInstance()
await i18n.init({ lng: 'en', fallbackLng: 'en', resources: { en, zh } })
const originals = { get: api.get, put: api.put, post: api.post }
const boundary = api as unknown as {
  get: (url: string) => Promise<unknown>
  put: (url: string, data: unknown) => Promise<unknown>
  post: (url: string, data: unknown, options: unknown) => Promise<unknown>
}
let root: Root
let queryClient: InstanceType<typeof QueryClient>
let container: HTMLDivElement
let actions: HTMLDivElement
let saved: { url: string; data: unknown }[] = []
let cleared: { url: string; options: unknown }[] = []

beforeEach(async () => {
  notifyManager.setScheduler(queueMicrotask)
  await i18n.changeLanguage('en')
  saved = []
  cleared = []
  boundary.get = async (url) => ({
    data: {
      success: true,
      data: url === '/api/performance/logs' ? { enabled: false } : null,
    },
  })
  boundary.put = async (url, data) => {
    saved.push({ url, data })
    return { data: { success: true } }
  }
  boundary.post = async (url, _data, options) => {
    cleared.push({ url, options })
    return {
      data: {
        success: true,
        data: {
          task_id: 'cleanup-fixture',
          status: 'succeeded',
          state: { total: 2, processed: 2, progress: 100, remaining: 0 },
          result: {
            deleted_count: 2,
            deleted_logs: 1,
            deleted_tasks: 1,
            skipped_tasks: 0,
          },
        },
      },
    }
  }
  container = document.createElement('div')
  actions = document.createElement('div')
  document.body.append(container, actions)
  root = createRoot(container)
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  queryClient.clear()
  container.remove()
  actions.remove()
  api.get = originals.get
  api.put = originals.put
  api.post = originals.post
  notifyManager.setScheduler(defaultScheduler)
  useAuthStore.setState((state) => ({ auth: { ...state.auth, user: null } }))
  document.body.innerHTML = ''
})
after(() => dom.happyDOM.cancelAsync())

async function renderSettings(role = 100) {
  useAuthStore.setState((state) => ({
    auth: {
      ...state.auth,
      user: { id: 1, username: 'cleanup-fixture', role } as NonNullable<
        typeof state.auth.user
      >,
    },
  }))
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <SettingsPageProvider actionsContainer={actions}>
            <LogSettingsSection defaultEnabled />
          </SettingsPageProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
  })
}
function button(label: string, scope: ParentNode = document) {
  return [...scope.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent?.trim() === label
  )
}

test('根用户在日志清理页面设置生成文件保留时长', async () => {
  await renderSettings()
  const field = container.querySelector<HTMLInputElement>('input[name="hours"]')
  assert.ok(field, '文件保存时长应出现在日志清理页面')
  assert.equal(field.value, '2')
  assert.equal(field.min, '1')
  assert.equal(field.max, '168')
  const setValue = Object.getOwnPropertyDescriptor(
    dom.HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(setValue)
  await act(async () => {
    setValue.call(field, '6')
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () => button('Save log settings')?.click())
  assert.ok(
    saved.some(
      (call) =>
        JSON.stringify(call.data) ===
        JSON.stringify({ key: 'AsyncMediaRetentionHours', value: 6 })
    )
  )
})

test('联合清理需要确认并说明任务日志和媒体都会删除', async () => {
  await renderSettings()
  const clear = button('Clean logs and tasks')
  assert.ok(clear)
  await act(async () => clear.click())
  const dialog = document.querySelector('[role="alertdialog"]')
  assert.ok(dialog)
  assert.match(dialog.textContent ?? '', /task logs/)
  assert.match(dialog.textContent ?? '', /stored media/)
  assert.equal(cleared.length, 0)
  await act(async () => button('Cancel', dialog)?.click())
  assert.equal(cleared.length, 0)
  await act(async () => clear.click())
  const confirmation = document.querySelector('[role="alertdialog"]')
  assert.ok(confirmation)
  await act(async () => button('Delete logs and tasks', confirmation)?.click())
  assert.equal(cleared.length, 1)
  assert.equal(cleared[0].url, '/api/system-task/log-cleanup')
  assert.ok(
    (cleared[0].options as { params: { target_timestamp: number } }).params
      .target_timestamp > 0
  )
  assert.match(container.textContent ?? '', /1 usage logs and 1 task logs/)
})

test('普通管理员保留查看权限但没有保存时长和联合删除入口', async () => {
  await renderSettings(10)
  assert.equal(container.querySelector('input[name="hours"]'), null)
  assert.equal(button('Clean logs and tasks')?.disabled, true)
})

test('中文页面的联合清理及文件时长文案加载正确', async () => {
  await i18n.changeLanguage('zh')
  await renderSettings()
  assert.ok(button('清理日志和任务'))
  assert.match(container.textContent ?? '', /文件保存时长|保存时长|保留时长/)
  assert.doesNotMatch(
    container.textContent ?? '',
    /Clean logs and tasks|Completed task logs|Retention period/
  )
})

// 无效时长只显示校验结果，不把空值或范围外的数值提交为新的保存策略。
test('保存时长超出范围时保留原设置', async () => {
  await renderSettings()
  const field = container.querySelector<HTMLInputElement>('input[name="hours"]')
  assert.ok(field)
  const setValue = Object.getOwnPropertyDescriptor(
    dom.HTMLInputElement.prototype,
    'value'
  )?.set
  assert.ok(setValue)
  await act(async () => {
    setValue.call(field, '169')
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () => button('Save log settings')?.click())
  assert.equal(field.getAttribute('aria-invalid'), 'true')
  assert.equal(saved.length, 0)
})
