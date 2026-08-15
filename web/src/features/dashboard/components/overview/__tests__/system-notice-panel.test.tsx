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

const domWindow = new Window()
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
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
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

mock.module('@/components/rich-content', () => ({
  RichContent: (props: { content: string }) =>
    createElement(
      'article',
      { 'data-testid': 'notice-content' },
      props.content
    ),
}))
mock.module('@/components/ui/scroll-area', () => ({
  ScrollArea: (props: { children?: ReactNode; className?: string }) =>
    createElement('div', { className: props.className }, props.children),
}))

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'System Notice': 'System Notice',
        'Latest platform updates and notices':
          'Latest platform updates and notices',
        'No announcements at this time': 'No announcements at this time',
      },
    },
  },
})

const { SystemNoticePanelView } = await import('../system-notice-panel')

describe('概览系统公告面板', () => {
  beforeEach(() => {
    document.body.replaceChildren()
  })

  after(() => domWindow.close())

  test('普通系统公告存在时在毛玻璃面板中展示正文', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <SystemNoticePanelView
            notice='Scheduled maintenance tonight'
            loading={false}
          />
        </I18nextProvider>
      )
    })

    assert.equal(container.textContent?.includes('System Notice'), true)
    assert.equal(
      container.querySelector('[data-testid="notice-content"]')?.textContent,
      'Scheduled maintenance tonight'
    )
    assert.ok(container.querySelector('.appearance-glass-surface'))

    await act(async () => root.unmount())
    container.remove()
  })

  test('普通系统公告为空时保留面板并显示明确空状态', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <SystemNoticePanelView notice='' loading={false} />
        </I18nextProvider>
      )
    })

    assert.equal(container.textContent?.includes('System Notice'), true)
    assert.equal(
      container.textContent?.includes('No announcements at this time'),
      true
    )
    assert.ok(container.querySelector('.appearance-glass-surface'))
    assert.equal(
      container.querySelector('[data-testid="notice-content"]'),
      null
    )

    await act(async () => root.unmount())
    container.remove()
  })
})
