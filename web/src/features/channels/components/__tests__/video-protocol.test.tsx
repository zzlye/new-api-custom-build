import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'

import { Window } from 'happy-dom'

// 只替换浏览器和 HTTP 边界，实际渲染根权限入口及协议编辑器。
const dom = new Window({ url: 'http://localhost/' })
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLSelectElement',
  'HTMLTextAreaElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'MutationObserver',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: dom[key],
  })
}
Object.defineProperty(globalThis, 'IS_REACT_ACT_ENVIRONMENT', {
  configurable: true,
  value: true,
})
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider, notifyManager } =
  await import('@tanstack/react-query')
const { I18nextProvider } = await import('react-i18next')
const { createInstance } = await import('i18next')
const { VideoProtocolEditor } = await import('../video-protocol-editor')
const { useAuthStore } = await import('@/stores/auth-store')
const { api } = await import('@/lib/api')
const translations = (await import('@/i18n/locales/en.json')).default
const i18n = createInstance()
await i18n.init({ lng: 'en', resources: { en: translations } })
const original = { get: api.get, put: api.put }
const boundary = api as unknown as {
  get: (url: string) => Promise<unknown>
  put: (url: string, data: unknown) => Promise<unknown>
}
let cleanup = async () => {}
afterEach(async () => {
  await cleanup()
  api.get = original.get
  api.put = original.put
})

async function render(role: number) {
  notifyManager.setScheduler(queueMicrotask)
  useAuthStore.setState({
    auth: {
      ...useAuthStore.getState().auth,
      user: { id: 1, username: 'fixture', role } as NonNullable<
        ReturnType<typeof useAuthStore.getState>['auth']['user']
      >,
    },
  })
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  cleanup = async () => {
    await act(async () => root.unmount())
    client.clear()
    container.remove()
  }
  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <VideoProtocolEditor channelId={8} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  )
  return container
}

test('普通管理员与普通用户均不显示协议入口且不读取规则', async () => {
  let calls = 0
  boundary.get = async () => {
    calls++
    return {}
  }
  for (const role of [0, 1, 10]) {
    const container = await render(role)
    assert.equal(container.textContent, '')
    assert.equal(calls, 0)
    await cleanup()
  }
  cleanup = async () => {}
})

test('根用户可读取预设、编辑时长映射并保存完整协议', async () => {
  const protocol = {
    enabled: false,
    submit_path: '/v1/videos',
    poll_path: '/v1/videos/{id}',
    poll_method: 'GET',
    poll_id_field: 'task_id',
    content_path: '',
    encoding: 'json',
    auth_mode: 'header',
    auth_name: 'Authorization',
    auth_prefix: 'Bearer ',
    fields: [{ source: 'duration', target: 'duration', format: 'identity' }],
    defaults: {},
    headers: {},
    response: {
      id: 'id',
      status: 'status',
      url: 'url',
      error: 'error.message',
      progress: 'progress',
      states: { success: 'completed' },
    },
  }
  const saves: { url: string; data: unknown }[] = []
  boundary.get = async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/8')
        ? protocol
        : { channels: [], presets: { standard: protocol } },
    },
  })
  boundary.put = async (url, data) => {
    saves.push({ url, data })
    return { data: { success: true, data } }
  }
  const container = await render(100)
  const details = container.querySelector('details')
  assert.ok(details)
  await act(async () => {
    details.open = true
    details.dispatchEvent(new Event('toggle', { bubbles: true }))
  })
  await act(async () => {
    await Promise.resolve()
  })
  const input = [...container.querySelectorAll('label')]
    .find((label) => label.textContent?.startsWith('Upstream field'))
    ?.querySelector('input')
  assert.ok(input)
  await act(async () => {
    const setValue = Object.getOwnPropertyDescriptor(
      dom.HTMLInputElement.prototype,
      'value'
    )?.set
    assert.ok(setValue)
    setValue.call(input, 'seconds')
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  const button = [...container.querySelectorAll('button')].find(
    (b) => b.textContent === 'Save video protocol'
  )
  assert.ok(button)
  await act(async () => button.click())
  assert.equal(saves.length, 1)
  assert.equal(saves[0].url, '/api/video-protocol/8')
  assert.equal((saves[0].data as typeof protocol).fields[0].target, 'seconds')
  assert.match(container.textContent ?? '', /Video protocol saved/)
})
