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
import { test } from 'node:test'

import { api } from '@/lib/api'

import { buildSearchParams } from '../filter'
import { fetchLogsByCategory } from '../utils'

test('任务模型筛选写入地址并去除空格，清空后不保留上次模型', () => {
  assert.deepEqual(
    buildSearchParams(
      { taskId: 'async_fixture', model: ' gpt-image-2 ' },
      'task'
    ),
    {
      filter: 'async_fixture',
      model: 'gpt-image-2',
    }
  )
  assert.deepEqual(buildSearchParams({ model: '   ' }, 'task'), {})
})

test('本人及全部任务查询均携带模型条件并保留分页和任务编号', async () => {
  const originalGet = api.get
  const requests: string[] = []
  // 只替换请求边界，实际使用页面到接口的完整参数映射。
  const boundary = api as unknown as { get: (url: string) => Promise<unknown> }
  boundary.get = async (url) => {
    requests.push(url)
    return { data: { success: true, data: { items: [], total: 0 } } }
  }
  try {
    for (const isAdmin of [false, true]) {
      await fetchLogsByCategory({
        logCategory: 'task',
        isAdmin,
        page: 2,
        pageSize: 20,
        searchParams: { model: 'gpt-image-2', filter: 'async_fixture' },
        columnFilters: [],
      })
    }
    for (const [index, request] of requests.entries()) {
      const url = new URL(request, 'http://localhost')
      assert.equal(url.pathname, index === 0 ? '/api/task/self' : '/api/task')
      assert.equal(url.searchParams.get('model_name'), 'gpt-image-2')
      assert.equal(url.searchParams.get('task_id'), 'async_fixture')
      assert.equal(url.searchParams.get('p'), '2')
    }
  } finally {
    api.get = originalGet
  }
})
