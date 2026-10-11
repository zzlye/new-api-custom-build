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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { UserQuotaDialog } from '../user-quota-dialog'

const i18n = createInstance()
await i18n.init({ lng: 'zh', resources: { zh }, initAsync: false })

function AdjustmentFixture() {
  const [open, setOpen] = useState(true)
  return (
    <I18nextProvider i18n={i18n}>
      <button type='button' onClick={() => setOpen(true)}>
        重新调整
      </button>
      <UserQuotaDialog
        open={open}
        onOpenChange={setOpen}
        userId={42}
        currentQuota={5000000}
        currentGiftQuota={2000000}
        onSuccess={() => {}}
      />
    </I18nextProvider>
  )
}

beforeEach(() =>
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
)
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('单独减少赠金时只提交赠金类别，普通余额保持原值', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  render(<AdjustmentFixture />)
  expect(screen.getByText(/普通余额.*6/)).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '赠金' }))
  expect(screen.getByRole('button', { name: '赠金' })).toHaveAttribute(
    'aria-pressed',
    'true'
  )
  await userEvent.click(
    screen.getByRole('button', { name: i18n.t('Subtract') })
  )
  await userEvent.type(screen.getByRole('spinbutton'), '1')
  await userEvent.click(screen.getByRole('button', { name: i18n.t('Confirm') }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/user/manage', {
      id: 42,
      action: 'add_quota',
      quota_type: 'gift',
      mode: 'subtract',
      value: 500000,
    })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

it('赠金不足时阻止请求，后台拒绝时保留输入方便修正', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: false, message: '赠金不足' } })
  render(<AdjustmentFixture />)
  await userEvent.click(screen.getByRole('button', { name: '赠金' }))
  await userEvent.click(
    screen.getByRole('button', { name: i18n.t('Subtract') })
  )
  const amount = screen.getByRole('spinbutton')
  await userEvent.type(amount, '5')
  await userEvent.click(screen.getByRole('button', { name: i18n.t('Confirm') }))
  expect(post).not.toHaveBeenCalled()
  await userEvent.clear(amount)
  await userEvent.type(amount, '2')
  await userEvent.click(screen.getByRole('button', { name: i18n.t('Confirm') }))
  await waitFor(() => expect(post).toHaveBeenCalledOnce())
  expect(screen.getByRole('dialog')).toBeInTheDocument()
  expect(amount).toHaveValue(2)
})

it('关闭再打开恢复普通余额，覆盖赠金时必须明确输入零', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  render(<AdjustmentFixture />)
  await userEvent.click(screen.getByRole('button', { name: '赠金' }))
  await userEvent.type(screen.getByRole('spinbutton'), '3')
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await userEvent.click(screen.getByRole('button', { name: '重新调整' }))
  expect(screen.getByRole('button', { name: '普通余额' })).toHaveAttribute(
    'aria-pressed',
    'true'
  )
  expect(screen.getByRole('spinbutton')).toHaveValue(null)
  await userEvent.click(screen.getByRole('button', { name: '赠金' }))
  await userEvent.click(
    screen.getByRole('button', { name: i18n.t('Override') })
  )
  expect(screen.getByRole('button', { name: i18n.t('Confirm') })).toBeDisabled()
  await userEvent.type(screen.getByRole('spinbutton'), '0')
  await userEvent.click(screen.getByRole('button', { name: i18n.t('Confirm') }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/manage',
      expect.objectContaining({
        quota_type: 'gift',
        mode: 'override',
        value: 0,
      })
    )
  )
})
