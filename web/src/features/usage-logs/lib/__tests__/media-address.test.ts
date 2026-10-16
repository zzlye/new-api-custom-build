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

import { taskAudioDownloadName, taskMediaAddress } from '../media-address'

describe('任务音频下载文件名', () => {
  test('音频没有原始文件名时按媒体格式补齐常见音频后缀', () => {
    for (const [type, extension] of [
      ['audio/mpeg', 'mp3'],
      ['audio/wav', 'wav'],
      ['audio/flac', 'flac'],
      ['audio/ogg', 'ogg'],
      ['audio/mp4', 'm4a'],
      ['audio/aac', 'aac'],
      ['audio/webm', 'webm'],
    ]) {
      assert.equal(
        taskAudioDownloadName(undefined, type, '', 1),
        `audio-2.${extension}`
      )
    }
  })
  test('原音频名称被保留且错误后缀以实际响应类型修正', () => {
    assert.equal(
      taskAudioDownloadName('配音.mp3', 'audio/wav', 'audio/mpeg', 0),
      '配音.wav'
    )
    assert.equal(
      taskAudioDownloadName('音乐.flac', '', 'audio/flac', 0),
      '音乐.flac'
    )
    assert.equal(
      taskAudioDownloadName(
        '配音',
        'application/octet-stream',
        'Audio/X-WAV; charset=binary',
        0
      ),
      '配音.wav'
    )
    assert.equal(taskAudioDownloadName('音乐.mp3', '', '', 0), '音乐.mp3')
  })
  test('下载文件名移除路径和非法字符，空名字有稳定后缀', () => {
    assert.equal(
      taskAudioDownloadName('目录/配音:1.wav', 'audio/wav', '', 0),
      '目录_配音_1.wav'
    )
    assert.equal(
      taskAudioDownloadName('CON.mp3', 'audio/mpeg', '', 0),
      '_CON.mp3'
    )
    assert.equal(
      taskAudioDownloadName('...', 'audio/flac', '', 0),
      'audio-1.flac'
    )
    assert.equal(taskAudioDownloadName(undefined, '', '', 0), 'audio-1.bin')
  })
})

describe('任务媒体地址', () => {
  const origin = 'https://localhost:8443'
  test('管理员配置的媒体域名和路径前缀用于复制及打开链接', () => {
    const base = 'https://media.example:8443/gateway/'
    const path = `/task-media/async_example/reference/0?expires=2000000000&signature=${'a'.repeat(64)}`
    assert.equal(
      taskMediaAddress(path, base, 'preview'),
      base.slice(0, -1) + path
    )
    assert.equal(
      taskMediaAddress(base.slice(0, -1) + path, base, 'preview'),
      base.slice(0, -1) + path
    )
    assert.equal(
      taskMediaAddress('/api/task/async_example/media/0', base, 'api'),
      `${base}api/task/async_example/media/0`
    )
    assert.equal(
      taskMediaAddress(`https://old.example${path}`, base, 'preview'),
      ''
    )
  })
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
