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
  'HTMLAnchorElement',
  'Node',
  'Element',
  'Event',
  'MouseEvent',
  'PointerEvent',
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

let parallaxEnabled = true
let animationFrameId = 0
const pendingAnimationFrames = new Map<number, FrameRequestCallback>()

Object.defineProperty(domWindow, 'matchMedia', {
  configurable: true,
  value: () => ({
    matches: parallaxEnabled,
    addEventListener() {},
    removeEventListener() {},
  }),
})
Object.defineProperty(domWindow, 'requestAnimationFrame', {
  configurable: true,
  value: (callback: FrameRequestCallback) => {
    animationFrameId += 1
    pendingAnimationFrames.set(animationFrameId, callback)
    return animationFrameId
  },
})
Object.defineProperty(domWindow, 'cancelAnimationFrame', {
  configurable: true,
  value: (id: number) => pendingAnimationFrames.delete(id),
})

function flushAnimationFrames() {
  for (const callback of pendingAnimationFrames.values()) {
    callback(performance.now())
  }
  pendingAnimationFrames.clear()
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

mock.module('@tanstack/react-router', () => ({
  Link: (props: { to: string; children?: ReactNode }) =>
    createElement('a', { href: props.to }, props.children),
  useRouterState: (options: {
    select: (state: {
      isLoading: boolean
      location: { pathname: string }
    }) => unknown
  }) =>
    options.select({
      isLoading: false,
      location: { pathname: '/' },
    }),
}))
mock.module('lucide-react', () => ({
  ArrowRight: () => createElement('span'),
  BookOpen: () => createElement('span'),
}))
mock.module('@/components/ui/button', () => ({
  Button: (props: {
    render?: { props?: { href?: string; to?: string } }
    children?: ReactNode
    className?: string
  }) => {
    const tagName = props.render ? 'a' : 'button'
    return createElement(
      tagName,
      {
        className: props.className,
        href: props.render?.props?.href || props.render?.props?.to,
      },
      props.children
    )
  },
}))
mock.module('@/hooks/use-status', () => ({
  useStatus: () => ({ status: { docs_link: 'https://docs.example.test' } }),
}))
mock.module('@/hooks/use-appearance', () => ({
  useAppearance: () => ({
    theme_preset: 'default',
    home_bg_type: 'none',
    home_bg_color: '',
    home_bg_media: '',
    home_bg_overlay_opacity: 0,
    login_bg_type: 'none',
    login_bg_color: '',
    login_bg_media: '',
    login_bg_overlay_opacity: 0,
  }),
}))
mock.module('@/lib/appearance', () => ({
  // 测试主页无背景时的回退分支，保持与真实背景辅助函数接口一致。
  getEffectiveHomeBackground: () => ({ type: 'none', color: '', media: '' }),
  getEffectiveHomeBackgroundOverlayOpacity: () => 0,
}))
mock.module('@/components/page-background', () => ({
  PageBackground: (props: { className?: string }) =>
    createElement('div', {
      className: props.className,
      'data-testid': 'home-background',
    }),
}))
mock.module('@/features/home/components/hero-terminal-demo', () => ({
  HeroTerminalDemo: () => createElement('div', { 'data-testid': 'hero-demo' }),
}))

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Unified API Gateway for': 'Gateway',
        'Vast Range of AI Models': 'Models',
        Docs: 'Docs',
        'Get Started': 'Start',
        'View Pricing': 'Pricing',
        'Go to Dashboard': 'Dashboard',
      },
    },
  },
})

const { Hero } = await import('../hero')

describe('单屏主页 Hero', () => {
  beforeEach(() => {
    parallaxEnabled = true
    pendingAnimationFrames.clear()
    document.body.replaceChildren()
  })

  after(() => domWindow.close())

  test('保留主要操作和 API 演示，并移除冗余介绍', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <Hero />
        </I18nextProvider>
      )
    })

    const hero = container.querySelector('section')
    assert.ok(hero)
    assert.equal(hero.classList.contains('min-h-svh'), true)
    assert.ok(container.querySelector('[data-testid="home-background"]'))
    assert.ok(container.querySelector('[data-testid="hero-demo"]'))
    assert.equal(
      container
        .querySelector('[data-testid="hero-demo"]')
        ?.parentElement?.classList.contains('hidden'),
      true
    )
    assert.equal(
      container.textContent?.includes('Supported Applications'),
      false
    )
    assert.equal(
      container
        .querySelector('a[href="/sign-up"]')
        ?.textContent?.includes('Start'),
      true
    )

    await act(async () => root.unmount())
    container.remove()
  })

  test('桌面鼠标移动时背景按半球弧线覆盖大范围视角', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <Hero />
        </I18nextProvider>
      )
    })

    const hero = container.querySelector<HTMLElement>('section')
    assert.ok(hero)
    hero.getBoundingClientRect = () => ({
      bottom: 800,
      height: 800,
      left: 0,
      right: 1000,
      top: 0,
      width: 1000,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })

    await act(async () => {
      hero.dispatchEvent(
        new domWindow.PointerEvent('pointermove', {
          bubbles: true,
          clientX: 1000,
          clientY: 0,
        }) as unknown as Event
      )
      flushAnimationFrames()
    })

    assert.equal(hero.dataset.homeParallaxActive, 'true')
    assert.equal(hero.style.getPropertyValue('--home-view-rotate-x'), '6.00deg')
    assert.equal(hero.style.getPropertyValue('--home-view-rotate-y'), '8.00deg')
    assert.equal(
      hero.style.getPropertyValue('--home-view-background-x'),
      '-32.00px'
    )
    assert.equal(
      hero.style.getPropertyValue('--home-view-background-y'),
      '20.00px'
    )
    assert.equal(
      container
        .querySelector('[data-testid="home-background"]')
        ?.classList.contains('home-hero-depth-background'),
      true
    )
    assert.equal(container.querySelector('.home-hero-depth-foreground'), null)

    await act(async () => {
      hero.dispatchEvent(
        new domWindow.PointerEvent('pointermove', {
          bubbles: true,
          clientX: 750,
          clientY: 400,
        }) as unknown as Event
      )
      flushAnimationFrames()
    })
    assert.equal(hero.style.getPropertyValue('--home-view-rotate-y'), '2.67deg')
    assert.equal(
      hero.style.getPropertyValue('--home-view-background-x'),
      '-10.67px'
    )

    await act(async () => {
      hero.dispatchEvent(
        new domWindow.PointerEvent('pointerleave') as unknown as Event
      )
    })
    assert.equal(hero.dataset.homeParallaxActive, undefined)
    assert.equal(hero.style.getPropertyValue('--home-view-rotate-x'), '')

    await act(async () => root.unmount())
    container.remove()
  })

  test('触控设备不启用鼠标视角效果', async () => {
    parallaxEnabled = false
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <Hero />
        </I18nextProvider>
      )
    })

    const hero = container.querySelector<HTMLElement>('section')
    assert.ok(hero)
    await act(async () => {
      hero.dispatchEvent(
        new domWindow.PointerEvent('pointermove', {
          bubbles: true,
          clientX: 500,
          clientY: 300,
        }) as unknown as Event
      )
      flushAnimationFrames()
    })

    assert.equal(hero.dataset.homeParallaxActive, undefined)
    assert.equal(hero.style.getPropertyValue('--home-view-rotate-y'), '')

    await act(async () => root.unmount())
    container.remove()
  })
})
