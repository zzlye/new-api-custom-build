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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
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

mock.module('lucide-react', () => ({
  Share2: () => createElement('span'),
}))
mock.module('@/components/copy-button', () => ({
  CopyButton: (props: { value: string; 'aria-label'?: string }) =>
    createElement(
      'button',
      {
        type: 'button',
        'aria-label': props['aria-label'],
        'data-copy-value': props.value,
      },
      props['aria-label']
    ),
}))
mock.module('@/components/ui/card', () => ({
  Card: (props: { children?: ReactNode }) =>
    createElement('section', null, props.children),
  CardContent: (props: { children?: ReactNode }) =>
    createElement('div', null, props.children),
}))
mock.module('@/components/ui/icon-badge', () => ({
  IconBadge: (props: { children?: ReactNode }) =>
    createElement('span', null, props.children),
}))
mock.module('@/components/ui/input', () => ({
  Input: (props: { value?: string; readOnly?: boolean }) =>
    createElement('input', props),
}))
mock.module('@/components/ui/skeleton', () => ({
  Skeleton: () => createElement('span'),
}))

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Referral Program': 'Referral Program',
        'Earn rewards when your referrals add funds. Rewards are added to your balance automatically.':
          'Earn rewards when your referrals add funds. Rewards are added to your balance automatically.',
        'Commission Rate': 'Commission Rate',
        Invites: 'Invites',
        'Copy referral link': 'Copy referral link',
      },
    },
  },
})

const { AffiliateRewardsCard } = await import('../affiliate-rewards-card')

describe('推荐计划卡片', () => {
  after(() => domWindow.close())

  test('将返佣比例显示为百分比并保留邀请数与链接', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <AffiliateRewardsCard
            user={{
              id: 1,
              username: 'tester',
              quota: 0,
              used_quota: 0,
              request_count: 0,
              aff_quota: 0,
              aff_history_quota: 100,
              aff_count: 3,
              group: 'default',
            }}
            affiliateLink='https://example.test/sign-up?aff=CODE'
            commissionRatio={0.15}
          />
        </I18nextProvider>
      )
    })

    assert.equal(container.textContent?.includes('Commission Rate'), true)
    assert.equal(container.textContent?.includes('15%'), true)
    assert.equal(container.textContent?.includes('Invites'), true)
    assert.equal(container.textContent?.includes('3'), true)
    assert.equal(container.textContent?.includes('Total Earned'), false)
    assert.equal(
      container.querySelector('input')?.value,
      'https://example.test/sign-up?aff=CODE'
    )

    await act(async () => {
      root.render(
        <I18nextProvider i18n={i18n}>
          <AffiliateRewardsCard
            user={{
              id: 1,
              username: 'tester',
              quota: 0,
              used_quota: 0,
              request_count: 0,
              aff_quota: 0,
              aff_history_quota: 100,
              aff_count: 3,
              group: 'default',
            }}
            affiliateLink='https://example.test/sign-up?aff=CODE'
            commissionRatio={0.155}
          />
        </I18nextProvider>
      )
    })
    assert.equal(container.textContent?.includes('15.5%'), true)

    await act(async () => root.unmount())
    container.remove()
  })
})
