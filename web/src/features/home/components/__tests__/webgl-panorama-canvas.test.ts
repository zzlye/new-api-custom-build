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

import {
  createPanoramaPatchData,
  projectPointerToPanorama,
} from '../webgl-panorama-math'

describe('主页半球全景网格', () => {
  test('完整生成球面顶点、纹理坐标和三角形索引', () => {
    const horizontalSegments = 4
    const verticalSegments = 2
    const radius = 10
    const patch = createPanoramaPatchData(
      horizontalSegments,
      verticalSegments,
      radius
    )

    assert.equal(patch.positions.length, 45)
    assert.equal(patch.uvs.length, 30)
    assert.equal(patch.indices.length, 48)
    assert.deepEqual(patch.uvs.slice(0, 2), new Float32Array([0, 1]))
    assert.deepEqual(patch.uvs.slice(-2), new Float32Array([1, 0]))

    const centerOffset = (1 * (horizontalSegments + 1) + 2) * 3
    assert.ok(Math.abs(patch.positions[centerOffset]) < 0.0001)
    assert.ok(Math.abs(patch.positions[centerOffset + 1]) < 0.0001)
    assert.ok(Math.abs(patch.positions[centerOffset + 2] + radius) < 0.0001)
  })

  test('鼠标中心保持正视，边缘达到最大半球环视角', () => {
    assert.deepEqual(projectPointerToPanorama(0, 0), { pitch: 0, yaw: 0 })

    const edge = projectPointerToPanorama(1, -1)
    assert.ok(Math.abs(edge.yaw - (32 * Math.PI) / 180) < 0.0001)
    assert.ok(Math.abs(edge.pitch + (16 * Math.PI) / 180) < 0.0001)

    const clamped = projectPointerToPanorama(5, -5)
    assert.deepEqual(clamped, edge)
  })
})
