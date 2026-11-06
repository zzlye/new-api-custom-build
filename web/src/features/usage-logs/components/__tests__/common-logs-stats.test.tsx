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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { CommonLogsFilterBar } from '../common-logs-filter-bar'
import { UsageLogsProvider } from '../usage-logs-provider'

function StatsFixture() {
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <UsageLogsProvider>
      <CommonLogsFilterBar table={table} />
    </UsageLogsProvider>
  )
}

async function renderStats(data: Record<string, number>) {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: { success: true, data: url === '/api/user/self/groups' ? {} : data },
  }))
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: StatsFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({
      initialEntries: ['/usage-logs/common?model=video&group=video-group'],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByText('Usage')
  return client
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  localStorage.clear()
})

it('在用量和RPM之间显示退款金额，隐藏金额时同步隐藏', async () => {
  const client = await renderStats({
    quota: 53500000,
    refund_quota: 36000000,
    rpm: 2,
    tpm: 8,
  })
  expect(screen.getByText('Refund')).toBeVisible()
  expect(screen.queryByText('Net usage')).not.toBeInTheDocument()
  const refund = screen.getByText('Refund')
  expect(
    screen.getByText('Usage').compareDocumentPosition(refund) &
      Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy()
  expect(
    refund.compareDocumentPosition(screen.getByText('RPM')) &
      Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy()
  for (const value of ['$107', '$72']) {
    expect(screen.getByText(value)).toBeVisible()
  }
  expect(api.get).toHaveBeenCalledWith(
    expect.stringMatching(/\/api\/log\/self\/stat\?.*model_name=video/)
  )
  await userEvent.click(screen.getByRole('button', { name: 'Hide' }))
  expect(screen.getAllByText('••••')).toHaveLength(2)
  for (const value of ['$107', '$72']) {
    expect(screen.queryByText(value)).not.toBeInTheDocument()
  }
  await userEvent.click(screen.getByRole('button', { name: 'Show' }))
  expect(screen.getByText('$72')).toBeVisible()
  client.clear()
})

it('时间范围内只有退款时仍显示退款金额', async () => {
  const client = await renderStats({
    quota: 0,
    refund_quota: 15000000,
    rpm: 0,
    tpm: 0,
  })
  expect(screen.getByText('$0')).toBeVisible()
  expect(screen.getByText('$30')).toBeVisible()
  client.clear()
})

it('旧接口没有退款字段时显示未知而不是错误的零退款', async () => {
  const client = await renderStats({ quota: 53500000, rpm: 0, tpm: 0 })
  await waitFor(() => expect(screen.getByText('—')).toBeVisible())
  expect(screen.getByText('$107')).toBeVisible()
  client.clear()
})
