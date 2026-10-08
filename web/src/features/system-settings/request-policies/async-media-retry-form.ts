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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseHttpStatusCodeRules } from '@/lib/http-status-code-rules'

export type AsyncMediaRetryValues = {
  enabled: boolean
  max_retries: number
  channel_ids: number[]
  channel_status_codes: Record<string, string>
  status_codes?: string
}

export const defaultAsyncMediaRetry: AsyncMediaRetryValues = {
  enabled: false,
  max_retries: 2,
  channel_status_codes: {},
  channel_ids: [] as number[],
}

export function createAsyncMediaRetrySchema(t: TFunction) {
  return z
    .object({
      enabled: z.boolean(),
      max_retries: z.number().int().min(0).max(20),
      // 旧通用字段只用于首次展示已有策略，提交时只保存渠道规则。
      status_codes: z.string().optional(),
      channel_status_codes: z.record(
        z.string().regex(/^[1-9]\d*$/),
        z.string().refine((value) => {
          if (!value.trim()) return true
          const parsed = parseHttpStatusCodeRules(value)
          return (
            parsed.ok &&
            parsed.ranges.length > 0 &&
            parsed.ranges.every(
              (range) => range.start >= 400 && range.end <= 599
            )
          )
        }, t('Enter HTTP error codes from 400 to 599, such as 500 or 500-503.'))
      ),
      channel_ids: z.array(z.number().int().positive()).max(10000),
    })
    .superRefine((value, context) => {
      if (
        value.enabled &&
        !value.status_codes &&
        !Object.values(value.channel_status_codes).some((codes) => codes.trim())
      ) {
        context.addIssue({
          code: 'custom',
          path: ['enabled'],
          message: t('Configure retry error codes for at least one channel.'),
        })
      }
    })
}
