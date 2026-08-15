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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'

const stylesheet = readFileSync(
  new URL('../index.css', import.meta.url),
  'utf8'
)

describe('视频背景毛玻璃', () => {
  test('动态背景不会覆盖外观设置的模糊效果', () => {
    assert.doesNotMatch(
      stylesheet,
      /:root\[data-video-background='true'\] body\s*{[^}]*--appearance-glass-backdrop-filter:\s*none;/s
    )
    assert.doesNotMatch(
      stylesheet,
      /:root\[data-video-background='true'\] \[data-slot='auth-surface'\]\s*{[^}]*(?:-webkit-)?backdrop-filter\s*:/s
    )
    assert.doesNotMatch(
      stylesheet,
      /:root\[data-video-background='true'\] \[class\*='backdrop-blur'\]\s*{[^}]*(?:-webkit-)?backdrop-filter:\s*none\s*!important;/s
    )
    assert.match(
      stylesheet,
      /\.appearance-glass-card,[^{]+{[^}]*backdrop-filter:\s*var\(\s*--appearance-glass-backdrop-filter,\s*saturate\(1\.15\) blur\(var\(--appearance-glass-blur, 16px\)\)\s*\);/s
    )
    assert.match(
      stylesheet,
      /\.appearance-glass-surface,[^{]+{[^}]*backdrop-filter:\s*var\(\s*--appearance-glass-backdrop-filter,\s*saturate\(1\.15\) blur\(var\(--appearance-glass-blur, 16px\)\)\s*\);/s
    )
    assert.doesNotMatch(
      stylesheet,
      /\.page-background-video\s*{[^}]*backface-visibility:/s
    )
  })
})
