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
export const DEFAULT_DOCS_LINK = 'https://zzlye.site/docs/index.html#/'

// 同时兼容旧版状态缓存和空配置，让各页面的文档入口保持一致。
export function resolveDocsLink(value?: unknown): string {
  const link = typeof value === 'string' ? value.trim() : ''
  if (!link) return DEFAULT_DOCS_LINK
  try {
    const { hostname } = new URL(link)
    if (hostname === 'docs.newapi.pro' || hostname === 'docs.newapi.ai') {
      return DEFAULT_DOCS_LINK
    }
  } catch {
    // 保留管理员设置的站内相对地址。
  }
  return link
}
