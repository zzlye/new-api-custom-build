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
import { cleanup, render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { WalletStatsCard } from '../wallet-stats-card'

const i18n = createInstance()
await i18n.init({ lng: 'zh', resources: { zh }, initAsync: false })
beforeEach(() =>
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
)
afterEach(cleanup)

it('钱包把赠金单列并显示推广计划文案', () => {
  render(
    <I18nextProvider i18n={i18n}>
      <WalletStatsCard
        user={{
          id: 42,
          username: 'wallet-gift',
          group: 'default',
          aff_quota: 0,
          aff_history_quota: 0,
          aff_count: 0,
          quota: 5000000,
          gift_quota: 2000000,
          used_quota: 1500000,
          request_count: 7,
        }}
      />
    </I18nextProvider>
  )
  expect(screen.getByText('赠金')).toBeInTheDocument()
  expect(screen.getByText('$4')).toBeInTheDocument()
  expect(screen.getByText('$10')).toBeInTheDocument()
  expect(screen.getByText('普通余额：$6')).toBeInTheDocument()
  expect(screen.getByText('推广计划')).toBeInTheDocument()
})

it('钱包未加载用户时显示零赠金并保持手机两列布局', () => {
  const { container, rerender } = render(
    <I18nextProvider i18n={i18n}>
      <WalletStatsCard user={null} />
    </I18nextProvider>
  )
  expect(screen.getByText('赠金')).toBeInTheDocument()
  expect(container.firstElementChild).toHaveClass(
    'grid-cols-2',
    'sm:grid-cols-4'
  )
  rerender(
    <I18nextProvider i18n={i18n}>
      <WalletStatsCard user={null} loading />
    </I18nextProvider>
  )
  expect(container.firstElementChild).toHaveClass(
    'grid-cols-2',
    'sm:grid-cols-4'
  )
})
