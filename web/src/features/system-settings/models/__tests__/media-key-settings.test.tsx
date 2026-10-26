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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { MediaKeySettings } from '../media-key-settings'

const i18n = createInstance()
await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })
let client: QueryClient

afterEach(() => {
  cleanup()
  client?.clear()
  vi.restoreAllMocks()
})

function Harness() {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setActions} />
      <SettingsPageProvider actionsContainer={actions}>
        <MediaKeySettings />
      </SettingsPageProvider>
    </>
  )
}

function renderSettings() {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <Harness />
      </I18nextProvider>
    </QueryClientProvider>
  )
}

test('root can reorder groups and save an empty list without inheriting ordinary Auto', async () => {
  let groups = ['vip', 'default']
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/group/'
          ? ['auto', 'default', 'vip']
          : { groups, models: [] },
    },
  }))
  const put = vi.spyOn(api, 'put').mockImplementation(async (_url, payload) => {
    groups = JSON.parse((payload as { value: string }).value)
    return { data: { success: true } }
  })
  renderSettings()
  await screen.findByRole('button', { name: 'Move default up' })
  expect(
    screen.queryByRole('button', { name: 'Restore global Auto' })
  ).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Move default up' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'MediaKeyGroups',
      value: '["default","vip"]',
    })
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled()
  )
  fireEvent.click(screen.getByRole('button', { name: 'Remove default' }))
  fireEvent.click(screen.getByRole('button', { name: 'Remove vip' }))
  expect(screen.getByText('No available groups')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenLastCalledWith('/api/option/', {
      key: 'MediaKeyGroups',
      value: '[]',
    })
  )
})

test('failed configuration loads can be retried and failed saves retain edits', async () => {
  let failedLoad = true
  vi.spyOn(api, 'get').mockImplementation(async () => {
    if (failedLoad) throw new Error('unavailable')
    return { data: { success: true, data: { groups: ['vip'], models: [] } } }
  })
  renderSettings()
  await screen.findByRole('button', { name: 'Retry' })
  failedLoad = false
  vi.mocked(api.get).mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/group/' ? ['vip'] : { groups: ['vip'], models: [] },
    },
  }))
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await screen.findByRole('button', { name: 'Remove vip' })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: false, message: 'rejected' } })
  fireEvent.click(screen.getByRole('button', { name: 'Remove vip' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(put).toHaveBeenCalled())
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  )
  expect(screen.getByText('No available groups')).toBeInTheDocument()
})
