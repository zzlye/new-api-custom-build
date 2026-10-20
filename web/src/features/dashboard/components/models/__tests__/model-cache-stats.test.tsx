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
import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { CacheHitRate } from '@/features/usage-logs/components/cache-hit-rate'
import { usageLogSchema } from '@/features/usage-logs/data/schema'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { getModelCacheStats } from '../../../api'
import { ModelCacheStatistics } from '../model-cache-stats'

vi.mock('../../../api', () => ({ getModelCacheStats: vi.fn() }))

const originalAuth = useAuthStore.getState()
const originalSystem = useSystemConfigStore.getState()
const filters = {
  start_timestamp: new Date(100000),
  end_timestamp: new Date(200000),
  username: 'other-user',
}
const stats = [
  {
    model_name: 'text-model',
    requests: 2,
    input_tokens: 1000,
    read_tokens: 140,
    write_tokens: 20,
    hit_rate: 14,
  },
]
const log = usageLogSchema.parse({
  id: 1,
  user_id: 7,
  created_at: 100,
  type: 2,
  content: '',
  prompt_tokens: 1000,
  cache_usage: stats[0],
})

beforeEach(() => {
  vi.mocked(getModelCacheStats).mockReset().mockResolvedValue(stats)
})

afterEach(() => {
  act(() => {
    useAuthStore.setState(originalAuth, true)
    useSystemConfigStore.setState(originalSystem, true)
  })
})

it.each([
  [100, false, true],
  [100, true, true],
  [10, false, false],
  [1, false, false],
  [10, true, true],
  [1, true, true],
  [0, true, false],
])(
  '角色 %s 公开 %s 时看板与日志同时控制可见性',
  async (role, isPublic, visible) => {
    useAuthStore.getState().auth.setUser({ id: 7, username: 'owner', role })
    useSystemConfigStore.getState().setConfig({ cacheStatsPublic: isPublic })
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <CacheHitRate log={log} />
        <ModelCacheStatistics filters={filters} />
      </QueryClientProvider>
    )
    if (!visible) {
      expect(screen.queryByText('Cache hit rate 14%')).not.toBeInTheDocument()
      expect(
        screen.queryByRole('region', { name: 'Model cache statistics' })
      ).not.toBeInTheDocument()
      expect(getModelCacheStats).not.toHaveBeenCalled()
      return
    }
    expect(screen.getByText('Cache hit rate 14%')).toBeVisible()
    expect(await screen.findByText('text-model')).toBeVisible()
    expect(screen.getByText('140')).toBeVisible()
    expect(screen.getByText('14%')).toBeVisible()
    expect(getModelCacheStats).toHaveBeenCalledWith(
      {
        start_timestamp: 100,
        end_timestamp: 200,
        username: role === 100 ? 'other-user' : undefined,
      },
      expect.any(AbortSignal)
    )
  }
)

it('关闭公开后普通用户不保留看板与百分比，根用户仍能重新查看', async () => {
  useAuthStore.getState().auth.setUser({ id: 7, username: 'owner', role: 1 })
  useSystemConfigStore.getState().setConfig({ cacheStatsPublic: true })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CacheHitRate log={log} />
      <ModelCacheStatistics filters={filters} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('text-model')).toBeVisible()
  act(() =>
    useSystemConfigStore.getState().setConfig({ cacheStatsPublic: false })
  )
  expect(screen.queryByText('text-model')).not.toBeInTheDocument()
  expect(screen.queryByText('Cache hit rate 14%')).not.toBeInTheDocument()
  act(() =>
    useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  )
  expect(await screen.findByText('text-model')).toBeVisible()
})

it('切换时间范围后查询新范围，查询失败不显示上次数据', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <ModelCacheStatistics filters={filters} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('text-model')).toBeVisible()
  vi.mocked(getModelCacheStats).mockRejectedValue(new Error('denied'))
  view.rerender(
    <QueryClientProvider client={client}>
      <ModelCacheStatistics
        filters={{ ...filters, start_timestamp: new Date(110000) }}
      />
    </QueryClientProvider>
  )
  await waitFor(() => expect(screen.getByText('denied')).toBeVisible())
  expect(screen.queryByText('text-model')).not.toBeInTheDocument()
})
