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
import { describe, test } from 'node:test'

import { buildSearchParams } from '../filter'
import { buildApiParams } from '../utils'

describe('日志详情筛选参数', () => {
  test('写入页面搜索参数时清理详情关键词', () => {
    const params = buildSearchParams({ detail: '  429  ' }, 'common')

    assert.equal(params.detail, '429')
  })

  test('请求日志接口时传递详情关键词', () => {
    const params = buildApiParams({
      page: 2,
      pageSize: 50,
      searchParams: { detail: 'rate_limit' },
      isAdmin: true,
    })

    assert.equal(params.detail, 'rate_limit')
  })
})
