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
const { VideoAdaptersSection } = await import('../index')
const { useAuthStore } = await import('@/stores/auth-store')
const { api } = await import('@/lib/api')
const translations = (await import('@/i18n/locales/en.json')).default
const i18n = createInstance()
await i18n.init({ lng: 'en', resources: { en: translations } })
const original = { get: api.get, put: api.put, post: api.post }
const boundary = api as unknown as {
  get: (url: string) => Promise<unknown>
  put: (url: string, data: unknown) => Promise<unknown>
  post: (url: string, data: unknown) => Promise<unknown>
}
let cleanup = async () => {}
afterEach(async () => {
  await cleanup()
  api.get = original.get
  api.put = original.put
  api.post = original.post
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
          <VideoAdaptersSection />
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

// 使用实际管理组件输入模型、添加新参数、预览和发布；仅替换 HTTP 边界。
test('根用户添加模型行、声明新字段、复制、预览和发布', async () => {
  const protocol = {
    enabled: true,
    submit_path: '/v1/videos',
    poll_path: '/v1/videos/{id}',
    poll_method: 'GET',
    poll_id_field: 'id',
    content_path: '',
    encoding: 'json',
    auth_mode: 'header',
    auth_name: 'Authorization',
    auth_prefix: 'Bearer ',
    fields: [
      { source: 'model', target: 'model', format: 'identity' },
      { source: 'prompt', target: 'prompt', format: 'identity' },
      { source: 'duration', target: 'seconds', format: 'number' },
    ],
    defaults: {},
    headers: {},
    response: {
      id: 'id',
      status: 'status',
      url: 'url',
      error: 'error.message',
      progress: 'progress',
      states: { completed: 'completed' },
    },
    capabilities: { duration: { default: 5 }, parameters: [] },
  }
  const registry = {
    version: 1,
    templates: [{ id: 'base', name: '通用模板', protocol }],
    rules: [],
  }
  const saves: unknown[] = []
  const previews: unknown[] = []
  boundary.get = async () => ({
    data: {
      success: true,
      data: {
        registry,
        channels: [{ id: 8, name: '测试渠道', models: 'existing' }],
      },
    },
  })
  boundary.put = async (_url, data) => {
    saves.push(data)
    return {
      data: { success: true, data: { ...(data as object), version: 2 } },
    }
  }
  boundary.post = async (url, data) => {
    assert.equal(url, '/api/video-adapters/preview')
    previews.push(data)
    return { data: { success: true, data: { output: { seconds: 5 } } } }
  }
  const host = await render(100)
  const button = (text: string) => {
    const b = [...host.querySelectorAll('button')].find(
      (b) => b.textContent === text
    )
    assert.ok(b, text)
    return b
  }
  const inputFor = (text: string) => {
    const label = [...host.querySelectorAll('label')].find((l) =>
      l.textContent?.startsWith(text)
    )
    assert.ok(label, text)
    const input = label.querySelector('input')
    assert.ok(input, text)
    return input
  }
  async function fill(
    input: HTMLInputElement | HTMLTextAreaElement,
    text: string
  ) {
    await act(async () => {
      const proto =
        input instanceof HTMLTextAreaElement
          ? dom.HTMLTextAreaElement.prototype
          : dom.HTMLInputElement.prototype
      Object.getOwnPropertyDescriptor(proto, 'value')?.set?.call(input, text)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
  }
  await act(async () => button('Add row').click())
  const channels = host.querySelector<HTMLSelectElement>(
    'article select[multiple]'
  )
  assert.ok(channels)
  await act(async () => {
    channels.options[0].selected = true
    channels.dispatchEvent(new Event('change', { bubbles: true }))
  })
  const names = host.querySelector<HTMLTextAreaElement>('article textarea')
  assert.ok(names)
  await fill(names, 'future-model,other-model')
  await act(async () =>
    names.dispatchEvent(new Event('focusout', { bubbles: true }))
  )
  await act(async () => inputFor('Customize this row').click())
  await act(async () => button('Add parameter').click())
  await fill(inputFor('Parameter key'), 'camera')
  await fill(inputFor('Display name'), '镜头方式')
  await fill(inputFor('Allowed values'), 'static,moving')
  await act(async () =>
    inputFor('Allowed values').dispatchEvent(
      new Event('focusout', { bubbles: true })
    )
  )
  await act(async () => button('Add mapping').click())
  const inputs = [...host.querySelectorAll('label')].filter((l) =>
    l.textContent?.startsWith('Input field')
  )
  const targets = [...host.querySelectorAll('label')].filter((l) =>
    l.textContent?.startsWith('Upstream field')
  )
  const sourceInput = inputs.at(-1)?.querySelector('input')
  const targetInput = targets.at(-1)?.querySelector('input')
  assert.ok(sourceInput)
  assert.ok(targetInput)
  await fill(sourceInput, 'extra_parameters.camera')
  await fill(targetInput, 'options.camera')
  await act(async () => button('Preview conversion').click())
  assert.equal(previews.length, 1)
  await act(async () => button('Duplicate').click())
  assert.equal(host.querySelectorAll('article').length, 2)
  await act(async () => button('Publish rules').click())
  assert.equal(saves.length, 1)
  const saved = saves[0] as {
    rules: {
      models: string[]
      enabled: boolean
      override: {
        capabilities: { parameters: { key: string; options: string[] }[] }
      }
    }[]
  }
  assert.deepEqual(saved.rules[0].models, ['future-model', 'other-model'])
  assert.equal(saved.rules[1].enabled, false)
  assert.equal(saved.rules[0].override.capabilities.parameters[0].key, 'camera')
  assert.deepEqual(saved.rules[0].override.capabilities.parameters[0].options, [
    'static',
    'moving',
  ])
  assert.match(host.textContent ?? '', /Video adapter rules published/)
})
