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

import { QUOTA_TYPES } from '../constants'
import type { PricingModel } from '../types'
import { filterByQuotaType } from './filters'

const createModel = (
  name: string,
  quotaType: number,
  billingMode?: string
): PricingModel => ({
  id: 1,
  model_name: name,
  quota_type: quotaType,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: ['default'],
  billing_mode: billingMode,
})

describe('模型计费类型筛选', () => {
  const models = [
    createModel('token-model', 0),
    createModel('request-model', 1),
    createModel('second-model', 1, 'per_second'),
  ]

  test('按秒模型不会混入按次模型', () => {
    assert.deepEqual(
      filterByQuotaType(models, QUOTA_TYPES.REQUEST).map(
        (model) => model.model_name
      ),
      ['request-model']
    )
  })

  test('按秒筛选只返回按秒模型', () => {
    assert.deepEqual(
      filterByQuotaType(models, QUOTA_TYPES.SECOND).map(
        (model) => model.model_name
      ),
      ['second-model']
    )
  })
})
