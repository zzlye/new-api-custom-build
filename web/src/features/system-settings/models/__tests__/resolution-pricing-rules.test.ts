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

import {
  buildResolutionExpression,
  hasValidResolutionPrices,
  readResolutionPrices,
} from '../resolution-pricing'

// 覆盖已发布模板，保证升级时还原的是旧表达式实际价格，而非猜测默认值。
test('旧的单条件模板保留其他分辨率的兜底价格', () => {
  assert.deepEqual(
    readResolutionPrices('param("resolution") == "480p" ? 0.23 : 0.4'),
    { '480p': '0.23', '720p': '0.4', '1080p': '0.4' }
  )
  assert.deepEqual(
    readResolutionPrices('param("resolution") == "720p" ? 0.4 : 0.23'),
    { '480p': '0.23', '720p': '0.4', '1080p': '0.23' }
  )
  assert.deepEqual(
    readResolutionPrices('param("resolution") == "1080p" ? 0.75 : 0.4'),
    { '480p': '0.4', '720p': '0.4', '1080p': '0.75' }
  )
})
test('三档价格序列化再回填保持数值不变', () => {
  const prices = { '480p': '0.23', '720p': '0.4', '1080p': '0.75' }
  assert.deepEqual(
    readResolutionPrices(buildResolutionExpression(prices)),
    prices
  )
})
test('极小的有效价格以科学计数法保存后仍能回填', () => {
  const prices = { '480p': '1e-8', '720p': '2e-8', '1080p': '3e-8' }
  assert.deepEqual(
    readResolutionPrices(buildResolutionExpression(prices)),
    prices
  )
})
test('未知或重复分支表达式不猜测价格', () => {
  assert.equal(
    readResolutionPrices('param("quality") == "high" ? 0.75 : 0.4'),
    null
  )
  assert.equal(
    readResolutionPrices(
      'param("resolution") == "480p" ? 0.2 : param("resolution") == "480p" ? 0.3 : 0.4'
    ),
    null
  )
})
test('缺失零负数及非有限值不生成可保存的规则', () => {
  for (const value of ['', ' ', '.', '0', '-1', 'Infinity', 'NaN']) {
    const prices = { '480p': value, '720p': '0.4', '1080p': '0.75' }
    assert.equal(hasValidResolutionPrices(prices), false)
    assert.equal(buildResolutionExpression(prices), '')
  }
})
