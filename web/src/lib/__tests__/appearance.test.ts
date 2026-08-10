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

import {
  DEFAULT_APPEARANCE,
  DEFAULT_GLASS_STRENGTH,
  getGlassProfile,
  getGlassStrength,
  normalizeAppearance,
  type AppearanceConfig,
} from '../appearance'

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

  test('全局背景在页面专属背景关闭时作为回退背景', async () => {
    const {
      getEffectiveHomeBackground,
      getEffectiveLoginBackground,
      hasEffectiveHomeBackground,
    } = await import('../appearance')
    const appearance = normalizeAppearance({
      global_bg_type: 'image',
      global_bg_media: '/uploads/global.jpg',
      global_bg_overlay_opacity: 0.2,
      glass_blur: 99,
      glass_opacity: -0.1,
    } as unknown as Partial<AppearanceConfig>)

    assert.deepEqual(getEffectiveHomeBackground(appearance), {
      type: 'image',
      color: '#0f172a',
      media: '/uploads/global.jpg',
    })
    assert.deepEqual(getEffectiveLoginBackground(appearance), {
      type: 'image',
      color: '#0f172a',
      media: '/uploads/global.jpg',
    })
    assert.equal(hasEffectiveHomeBackground(appearance), true)
    assert.equal(appearance.glass_blur, 40)
    assert.equal(appearance.glass_opacity, 0)
  })

  test('页面专属背景启用时覆盖全局背景', async () => {
    const { getEffectiveHomeBackground } = await import('../appearance')
    const appearance = normalizeAppearance({
      global_bg_type: 'solid',
      global_bg_color: '#111111',
      home_bg_type: 'solid',
      home_bg_color: '#222222',
    } as unknown as Partial<AppearanceConfig>)

    assert.deepEqual(getEffectiveHomeBackground(appearance), {
      type: 'solid',
      color: '#222222',
      media: '',
    })
  })
})

describe('毛玻璃简化设置', () => {
  test('旧版默认参数映射到稳定档位且往返不改变数值', () => {
    const profile = {
      glass_opacity: DEFAULT_APPEARANCE.glass_opacity,
      glass_border_opacity: DEFAULT_APPEARANCE.glass_border_opacity,
      glass_shadow_opacity: DEFAULT_APPEARANCE.glass_shadow_opacity,
    }

    const strength = getGlassStrength(profile)

    assert.equal(strength, 50)
    assert.deepEqual(getGlassProfile(strength), profile)
  })

  test('当前高强度参数保持在最高档位', () => {
    const profile = {
      glass_opacity: 0.76,
      glass_border_opacity: 1,
      glass_shadow_opacity: 0.195,
    }

    assert.equal(getGlassStrength(profile), 100)
    assert.deepEqual(getGlassProfile(100), profile)
  })

  test('推荐强度精确映射到当前确认的舒适参数', () => {
    const profile = {
      glass_opacity: 0.7,
      glass_border_opacity: 0.84,
      glass_shadow_opacity: 0.15,
    }

    assert.equal(getGlassStrength(profile), DEFAULT_GLASS_STRENGTH)
    assert.deepEqual(getGlassProfile(DEFAULT_GLASS_STRENGTH), profile)
  })

  test('非法历史参数回退到推荐档位', () => {
    assert.equal(
      getGlassStrength({
        glass_opacity: Number.NaN,
        glass_border_opacity: 0.5,
        glass_shadow_opacity: 0.1,
      }),
      DEFAULT_GLASS_STRENGTH
    )
  })
})
