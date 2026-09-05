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

// 本地链接只指向本站的任务内容；来源链接仅接受普通网络协议，避免复制临时地址或泄露认证信息。
export function taskMediaAddress(
  value: string | undefined,
  origin: string,
  kind: TaskMediaAddressKind
): string {
  if (!value) return ''
  try {
    const address = new URL(value, origin)
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
    const prefix = kind === 'preview' ? '/task-media/' : '/api/task/'
    if (
      address.origin !== origin ||
      address.search ||
      address.hash ||
      !address.pathname.startsWith(prefix)
    ) {
      return ''
    }
    return address.href
  } catch {
    return ''
  }
}
