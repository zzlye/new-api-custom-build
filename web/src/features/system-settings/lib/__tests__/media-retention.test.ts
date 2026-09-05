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

import { mediaRetentionSchema } from '../media-retention'

// 设置范围与服务端保持一致，禁止空值和非整小时值意外改变清理周期。
describe('生成文件保存时长', () => {
  test('默认值和范围边界都可保存', () => {
    for (const hours of [1, 2, 168]) {
      assert.equal(mediaRetentionSchema.safeParse({ hours }).success, true)
    }
  })
  test('空值、小数、越界值和非有限数字均提示校验错误', () => {
    for (const hours of [
      '',
      0,
      -1,
      169,
      1.5,
      Number.NaN,
      Number.POSITIVE_INFINITY,
    ]) {
      assert.equal(mediaRetentionSchema.safeParse({ hours }).success, false)
    }
  })
})
