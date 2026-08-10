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

import { MOTION_VARIANTS } from '../motion'

describe('页面切换动画', () => {
  test('不对整个页面使用模糊滤镜', () => {
    assert.equal('filter' in MOTION_VARIANTS.pageEnter.initial, false)
    assert.equal('filter' in MOTION_VARIANTS.pageEnter.animate, false)
    assert.equal('filter' in MOTION_VARIANTS.pageEnter.exit, false)
  })
})
