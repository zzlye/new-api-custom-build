/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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

import { normalizeAppearance, type AppearanceConfig } from '../appearance'

describe('页面背景遮罩透明度', () => {
  test('将接口返回的字符串透明度转换为 0 到 1 之间的数字', () => {
    const appearance = normalizeAppearance({
      home_bg_overlay_opacity: '0.65',
      login_bg_overlay_opacity: 1.4,
    } as unknown as Partial<AppearanceConfig>)

    assert.equal(appearance.home_bg_overlay_opacity, 0.65)
    assert.equal(appearance.login_bg_overlay_opacity, 1)
  })

  test('缺失或非法透明度时默认关闭遮罩', () => {
    const appearance = normalizeAppearance({
      home_bg_overlay_opacity: 'not-a-number',
      login_bg_overlay_opacity: -0.2,
    } as unknown as Partial<AppearanceConfig>)

    assert.equal(appearance.home_bg_overlay_opacity, 0)
    assert.equal(appearance.login_bg_overlay_opacity, 0)
  })
})
