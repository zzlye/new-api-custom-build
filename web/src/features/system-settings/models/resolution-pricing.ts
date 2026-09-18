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
export const VIDEO_RESOLUTIONS = ['480p', '720p', '1080p'] as const
export type ResolutionPrices = Record<
  (typeof VIDEO_RESOLUTIONS)[number],
  string
>
export const EMPTY_RESOLUTION_PRICES: ResolutionPrices = {
  '480p': '',
  '720p': '',
  '1080p': '',
}

// 只解析本编辑器生成的分辨率条件链，兼容旧模板的单条件及末尾兜底价格。
// 不执行任意表达式，遇到自定义规则时留空并要求明确填写，防止静默覆盖原价。
export function readResolutionPrices(
  expression: string
): ResolutionPrices | null {
  if (!expression.trim()) return null
  const number = String.raw`(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?`
  const branch = new RegExp(
    String.raw`^param\(\s*"resolution"\s*\)\s*==\s*"(480p|720p|1080p)"\s*\?\s*(${number})\s*:\s*`
  )
  const prices: Partial<ResolutionPrices> = {}
  let rest = expression.trim()
  let match = rest.match(branch)
  while (match) {
    const resolution = match[1] as keyof ResolutionPrices
    if (prices[resolution] !== undefined) return null
    prices[resolution] = String(Number(match[2]))
    rest = rest.slice(match[0].length).trim()
    match = rest.match(branch)
  }
  if (!new RegExp(`^${number}$`).test(rest)) return null
  const fallback = String(Number(rest))
  return {
    '480p': prices['480p'] ?? fallback,
    '720p': prices['720p'] ?? fallback,
    '1080p': prices['1080p'] ?? fallback,
  }
}

export function hasValidResolutionPrices(prices: ResolutionPrices): boolean {
  return VIDEO_RESOLUTIONS.every(
    (key) =>
      prices[key].trim() !== '' &&
      Number.isFinite(Number(prices[key])) &&
      Number(prices[key]) > 0
  )
}

// 从三档价格统一生成保存规则，避免切换模式时依赖过期的表达式状态。
export function buildResolutionExpression(prices: ResolutionPrices): string {
  if (!hasValidResolutionPrices(prices)) return ''
  return `param("resolution") == "1080p" ? ${Number(prices['1080p'])} : param("resolution") == "720p" ? ${Number(prices['720p'])} : ${Number(prices['480p'])}`
}
