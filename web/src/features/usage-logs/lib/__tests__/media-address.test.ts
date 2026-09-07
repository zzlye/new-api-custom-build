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

import { taskMediaAddress } from '../media-address'

describe('任务媒体地址', () => {
  const origin = 'https://localhost:8443'
  test('本站文件直链保留文件级签名，拒绝混入账户密钥和重复有效期', () => {
    const path = '/task-media/async_example/media/0'
    const signed = `${path}?expires=2000000000&signature=${'a'.repeat(64)}`
    assert.equal(taskMediaAddress(signed, origin, 'preview'), origin + signed)
    for (const invalid of [
      `${signed}&api_key=private`,
      `${signed}&expires=2000000001`,
      `${path}?signature=short`,
      `${path}?expires=1&signature=short`,
    ]) {
      assert.equal(taskMediaAddress(invalid, origin, 'preview'), '')
    }
  })
  test('本地地址保留当前页面协议和端口，来源保留签名参数', () => {
    assert.equal(
      taskMediaAddress('/task-media/async_example/media/0', origin, 'preview'),
      'https://localhost:8443/task-media/async_example/media/0'
    )
    assert.equal(
      taskMediaAddress('/api/task/async_example/media/0', origin, 'api'),
      'https://localhost:8443/api/task/async_example/media/0'
    )
    assert.equal(
      taskMediaAddress(
        'https://images.example/image.png?signature=fixture',
        origin,
        'source'
      ),
      'https://images.example/image.png?signature=fixture'
    )
  })
  test('临时地址、脚本协议和含登录凭据的来源不成为可点击链接', () => {
    for (const source of [
      undefined,
      '',
      'blob:fixture',
      'data:image/png;base64,fixture',
      'javascript:alert(1)',
      '//images.example/a.png',
      'https://name:secret@images.example/a.png',
    ]) {
      assert.equal(taskMediaAddress(source, origin, 'source'), '')
    }
  })
  test('本地链接拒绝跨站、凭据查询参数和其他页面路径', () => {
    assert.equal(
      taskMediaAddress(
        'https://other.example/task-media/async_example/media/0',
        origin,
        'preview'
      ),
      ''
    )
    assert.equal(
      taskMediaAddress(
        '/api/task/async_example/media/0?api_key=private',
        origin,
        'api'
      ),
      ''
    )
    assert.equal(taskMediaAddress('/profile', origin, 'preview'), '')
    assert.equal(
      taskMediaAddress(
        '/task-media/async_example/media/0#secret',
        origin,
        'preview'
      ),
      ''
    )
  })
})
