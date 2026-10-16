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
export type TaskMediaAddressKind = 'preview' | 'api' | 'source'

// 音频下载以实际响应类型确定后缀，通用二进制响应则沿用保存的媒体类型。
const audioExtensions: Record<string, string> = {
  'audio/mpeg': 'mp3',
  'audio/mp3': 'mp3',
  'audio/wav': 'wav',
  'audio/wave': 'wav',
  'audio/x-wav': 'wav',
  'audio/vnd.wave': 'wav',
  'audio/flac': 'flac',
  'audio/x-flac': 'flac',
  'audio/ogg': 'ogg',
  'application/ogg': 'ogg',
  'audio/opus': 'opus',
  'audio/mp4': 'm4a',
  'audio/x-m4a': 'm4a',
  'audio/aac': 'aac',
  'audio/x-aac': 'aac',
  'audio/aiff': 'aiff',
  'audio/x-aiff': 'aiff',
  'audio/webm': 'webm',
  'audio/basic': 'au',
}

export function taskAudioDownloadName(
  name: string | undefined,
  responseType: string,
  storedType: string,
  index: number
): string {
  const actualType = responseType.split(';')[0].trim().toLowerCase()
  const fallbackType = storedType.split(';')[0].trim().toLowerCase()
  let extension = audioExtensions[actualType] || audioExtensions[fallbackType]
  // 原始名字只用于下载命名，移除路径、控制字符及系统不接受的文件名字符。
  let filename = (name || '')
    .replaceAll(/[<>:"/\\|?*\p{Cc}]/gu, '_')
    .trim()
    .replace(/[. ]+$/, '')
  const existingExtension = /\.([a-z0-9]{1,10})$/i.exec(filename)
  if (!extension && existingExtension) {
    const suffix = existingExtension[1].toLowerCase()
    if (Object.values(audioExtensions).includes(suffix)) extension = suffix
  }
  if (existingExtension) {
    filename = filename.slice(0, -existingExtension[0].length)
  }
  filename = filename.replace(/[. ]+$/, '').slice(0, 180)
  if (!filename) filename = `audio-${index + 1}`
  if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(filename)) {
    filename = `_${filename}`
  }
  return `${filename}.${extension || 'bin'}`
}

// 本地链接只指向本站的任务内容；来源链接仅接受普通网络协议，避免复制临时地址或泄露认证信息。
export function taskMediaAddress(
  value: string | undefined,
  origin: string,
  kind: TaskMediaAddressKind
): string {
  if (!value) return ''
  try {
    const base = new URL(origin)
    const basePath = base.pathname.replace(/\/+$/, '')
    // 相对接口路径保留部署前缀；绝对地址必须属于管理员配置的媒体站点。
    const address = new URL(
      value.startsWith('/') && !value.startsWith('//')
        ? `${base.origin}${basePath}${value}`
        : value,
      base
    )
    if (
      !['http:', 'https:'].includes(address.protocol) ||
      address.username ||
      address.password
    ) {
      return ''
    }
    if (kind === 'source') {
      return value.toLowerCase().startsWith('http://') ||
        value.toLowerCase().startsWith('https://')
        ? address.href
        : ''
    }
    const prefix =
      basePath + (kind === 'preview' ? '/task-media/' : '/api/task/')
    if (
      address.origin !== base.origin ||
      address.hash ||
      !address.pathname.startsWith(prefix)
    ) {
      return ''
    }
    // 文件直链只允许服务器签发的文件级签名，不把账户密钥或其他查询参数放进链接。
    if (address.search) {
      if (kind !== 'preview') return ''
      const parameters = address.searchParams
      if (
        [...parameters.keys()].some(
          (key) => !['expires', 'signature'].includes(key)
        ) ||
        parameters.getAll('expires').length !== 1 ||
        parameters.getAll('signature').length !== 1 ||
        !/^[1-9]\d{0,12}$/.test(parameters.get('expires') || '') ||
        !/^[a-f0-9]{64}$/.test(parameters.get('signature') || '')
      ) {
        return ''
      }
    }
    return address.href
  } catch {
    return ''
  }
}
