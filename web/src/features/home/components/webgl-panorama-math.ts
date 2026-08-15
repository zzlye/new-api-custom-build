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

const HORIZONTAL_SPAN = (200 * Math.PI) / 180
const VERTICAL_SPAN = (125 * Math.PI) / 180
const MAX_YAW = (32 * Math.PI) / 180
const MAX_PITCH = (16 * Math.PI) / 180

export type PanoramaPatchData = {
  indices: Uint16Array
  positions: Float32Array
  uvs: Float32Array
}

/** 将普通宽屏媒体完整铺到观察者前方的内凹球面。 */
export function createPanoramaPatchData(
  horizontalSegments = 72,
  verticalSegments = 44,
  radius = 10
): PanoramaPatchData {
  const columns = horizontalSegments + 1
  const positions = new Float32Array(columns * (verticalSegments + 1) * 3)
  const uvs = new Float32Array(columns * (verticalSegments + 1) * 2)
  const indices = new Uint16Array(horizontalSegments * verticalSegments * 6)
  let positionOffset = 0
  let uvOffset = 0
  let indexOffset = 0

  for (let row = 0; row <= verticalSegments; row += 1) {
    const v = row / verticalSegments
    const pitch = (0.5 - v) * VERTICAL_SPAN
    const cosPitch = Math.cos(pitch)

    for (let column = 0; column <= horizontalSegments; column += 1) {
      const u = column / horizontalSegments
      const yaw = (u - 0.5) * HORIZONTAL_SPAN

      positions[positionOffset] = radius * Math.sin(yaw) * cosPitch
      positions[positionOffset + 1] = radius * Math.sin(pitch)
      positions[positionOffset + 2] = -radius * Math.cos(yaw) * cosPitch
      positionOffset += 3

      uvs[uvOffset] = u
      uvs[uvOffset + 1] = 1 - v
      uvOffset += 2
    }
  }

  for (let row = 0; row < verticalSegments; row += 1) {
    for (let column = 0; column < horizontalSegments; column += 1) {
      const topLeft = row * columns + column
      const bottomLeft = topLeft + columns
      indices[indexOffset] = topLeft
      indices[indexOffset + 1] = bottomLeft
      indices[indexOffset + 2] = topLeft + 1
      indices[indexOffset + 3] = topLeft + 1
      indices[indexOffset + 4] = bottomLeft
      indices[indexOffset + 5] = bottomLeft + 1
      indexOffset += 6
    }
  }

  return { indices, positions, uvs }
}

function clampUnit(value: number): number {
  return Math.min(1, Math.max(-1, value))
}

/** 用半球弧线放大屏幕边缘的鼠标视角变化。 */
export function projectPointerToPanorama(
  normalizedX: number,
  normalizedY: number
): { pitch: number; yaw: number } {
  const domeX = Math.asin(clampUnit(normalizedX)) / (Math.PI / 2)
  const domeY = Math.asin(clampUnit(normalizedY)) / (Math.PI / 2)

  return {
    pitch: domeY * MAX_PITCH,
    yaw: domeX * MAX_YAW,
  }
}
