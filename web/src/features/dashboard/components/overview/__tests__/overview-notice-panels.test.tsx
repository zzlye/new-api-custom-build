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
import { after, beforeEach, describe, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window({ url: 'http://localhost/dashboard/overview' })
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act, createElement } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const bunTestModule = 'bun:test'
const { mock } = (await import(bunTestModule)) as {
  mock: {
    module: (specifier: string, factory: () => Record<string, unknown>) => void
  }
}
type ReactNode = import('react').ReactNode

;(
  globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true

let announcementsVisible = true

const Icon = () => createElement('span')
mock.module('lucide-react', () => ({
  ArrowRight: Icon,
  Bell: Icon,
  BookOpen: Icon,
  Check: Icon,
  ChevronDown: Icon,
  ChevronUp: Icon,
  Circle: Icon,
  Copy: Icon,
  CreditCard: Icon,
  FileText: Icon,
  KeyRound: Icon,
  ListChecks: Icon,
  RadioTower: Icon,
  ShieldCheck: Icon,
  TerminalSquare: Icon,
  Timer: Icon,
}))
mock.module('@tanstack/react-query', () => ({
  useQuery: (options: { queryKey: string[] }) => {
    if (options.queryKey.includes('notice')) {
      return { data: { success: true, data: '' }, isLoading: false }
    }
    if (options.queryKey.includes('api-keys')) {
      return {
        data: [{ id: 1, key: 'test-key', name: 'Primary', status: 1 }],
        isFetched: true,
      }
    }
    return { data: ['test-model'], isFetched: true }
  },
}))
mock.module('@tanstack/react-router', () => ({
  Link: (props: { children?: ReactNode; to: string }) =>
    createElement('a', { href: props.to }, props.children),
}))
mock.module('motion/react', () => ({
  motion: {
    div: (props: { children?: ReactNode }) =>
      createElement('div', null, props.children),
  },
  useReducedMotion: () => true,
}))
mock.module('sonner', () => ({
  toast: { error() {}, success() {} },
}))
mock.module('@/components/page-transition', () => ({
  CardStaggerContainer: (props: { children?: ReactNode; className?: string }) =>
    createElement('section', { className: props.className }, props.children),
  CardStaggerItem: (props: { children?: ReactNode; className?: string }) =>
    createElement('div', { className: props.className }, props.children),
}))
mock.module('@/components/ui/button', () => ({
  Button: (props: { children?: ReactNode }) =>
    createElement('button', { type: 'button' }, props.children),
}))
mock.module('@/components/ui/icon-badge', () => ({
  IconBadge: (props: { children?: ReactNode }) =>
    createElement('span', null, props.children),
}))
mock.module('@/components/rich-content', () => ({
  RichContent: (props: { content: string }) =>
    createElement('article', null, props.content),
}))
mock.module('@/components/ui/scroll-area', () => ({
  ScrollArea: (props: { children?: ReactNode; className?: string }) =>
    createElement('div', { className: props.className }, props.children),
}))
mock.module('@/features/keys/api', () => ({
  fetchTokenKey: async () => ({ success: true, data: { key: 'test-key' } }),
  getApiKeys: async () => ({ success: true, data: { items: [] } }),
}))
mock.module('@/hooks/use-copy-to-clipboard', () => ({
  useCopyToClipboard: () => ({ copyToClipboard: async () => true }),
}))
mock.module('@/lib/api', () => ({
  getNotice: async () => ({ success: true, data: '' }),
  getUserModels: async () => ({ success: true, data: ['test-model'] }),
}))
mock.module('@/stores/auth-store', () => ({
  useAuthStore: (
    selector: (state: {
      auth: {
        user: {
          quota: number
          request_count: number
          role: number
          used_quota: number
        }
      }
    }) => unknown
  ) =>
    selector({
      auth: {
        user: { quota: 100, request_count: 1, role: 1, used_quota: 1 },
      },
    }),
}))
mock.module('../../../hooks/use-status-data', () => ({
  useApiInfo: () => ({ items: [] }),
  useDashboardContentVisibility: () => ({
    announcements: announcementsVisible,
  }),
}))
mock.module('../announcements-panel', () => ({
  AnnouncementsPanel: () =>
    createElement('div', { 'data-testid': 'scheduled-announcements' }),
}))
mock.module('../performance-health-panel', () => ({
  PerformanceHealthPanel: () => createElement('div'),
}))
mock.module('../summary-cards', () => ({
  SummaryCards: () => createElement('div'),
}))

const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'en', resources: { en: {} } })

const { OverviewDashboard } = await import('../overview-dashboard')

describe('概览公告面板组合', () => {
  beforeEach(() => {
    announcementsVisible = true
    window.localStorage.clear()
    document.body.replaceChildren()
  })

  after(() => domWindow.close())

  test('定时公告开启时同时展示普通系统公告和定时公告', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <OverviewDashboard />
        </I18nextProvider>
      )
    })

    assert.equal(container.textContent?.includes('System Notice'), true)
    assert.ok(
      container.querySelector('[data-testid="scheduled-announcements"]')
    )

    await act(async () => root.unmount())
    container.remove()
  })

  test('定时公告关闭时仍展示普通系统公告', async () => {
    announcementsVisible = false
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <OverviewDashboard />
        </I18nextProvider>
      )
    })

    assert.equal(container.textContent?.includes('System Notice'), true)
    assert.equal(
      container.querySelector('[data-testid="scheduled-announcements"]'),
      null
    )

    await act(async () => root.unmount())
    container.remove()
  })
})
