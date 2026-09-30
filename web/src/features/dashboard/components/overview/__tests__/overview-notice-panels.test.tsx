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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import {
  afterAll,
  beforeAll,
  afterEach,
  beforeEach,
  expect,
  it,
  vi,
} from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { OverviewDashboard } from '../overview-dashboard'

// 补齐测试浏览器缺少的动画查询 API，不替换真实滚动组件。
const animationDescriptor = Object.getOwnPropertyDescriptor(
  Element.prototype,
  'getAnimations'
)
beforeAll(() => {
  Object.defineProperty(Element.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})
afterAll(() => {
  if (animationDescriptor) {
    Object.defineProperty(
      Element.prototype,
      'getAnimations',
      animationDescriptor
    )
  } else {
    Reflect.deleteProperty(Element.prototype, 'getAnimations')
  }
})

let client: QueryClient
let announcementsVisible = true

beforeEach(() => {
  window.localStorage.clear()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'notice-user',
    role: 1,
    quota: 100,
    used_quota: 1,
    request_count: 1,
  })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  announcementsVisible = true
  // 只替换网络边界，公告面板、布局和状态读取均使用真实组件。
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/status':
        return {
          data: {
            success: true,
            data: {
              api_info_enabled: false,
              announcements_enabled: announcementsVisible,
              announcements: [
                { id: 'notice', content: 'Scheduled notice fixture' },
              ],
              faq_enabled: false,
              uptime_kuma_enabled: false,
            },
          },
        }
      case '/api/notice':
        return { data: { success: true, data: 'System notice fixture' } }
      case '/api/token/?p=1&size=10':
        return { data: { success: true, data: { items: [] } } }
      case '/api/user/models':
      case '/api/data/self':
        return { data: { success: true, data: [] } }
      default:
        throw new Error(`Unexpected request: ${url}`)
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  window.localStorage.clear()
})

async function renderOverview() {
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

it('定时公告开启时同时展示普通系统公告和定时公告', async () => {
  await renderOverview()
  await waitFor(() =>
    expect(screen.getByText('System notice fixture')).toBeVisible()
  )
  await waitFor(() =>
    expect(screen.getByText('Scheduled notice fixture')).toBeVisible()
  )
})

it('定时公告关闭时仍展示普通系统公告', async () => {
  announcementsVisible = false
  await renderOverview()
  await waitFor(() =>
    expect(screen.getByText('System notice fixture')).toBeVisible()
  )
  expect(screen.queryByText('Scheduled notice fixture')).not.toBeInTheDocument()
  expect(screen.queryByText('Announcements')).not.toBeInTheDocument()
})
