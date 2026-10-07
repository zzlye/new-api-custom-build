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

export const defaultAsyncMediaRetry = {
  enabled: false,
  max_retries: 2,
  status_codes: '429,500,502,503',
  channel_ids: [] as number[],
}

export function createAsyncMediaRetrySchema(t: TFunction) {
  return z.object({
    enabled: z.boolean(),
    max_retries: z.number().int().min(0).max(20),
    status_codes: z.string().refine((value) => {
      const parsed = parseHttpStatusCodeRules(value)
      return (
        parsed.ok &&
        parsed.ranges.length > 0 &&
        parsed.ranges.every((range) => range.start >= 400 && range.end <= 599)
      )
    }, t('Enter HTTP error codes from 400 to 599, such as 500 or 500-503.')),
    channel_ids: z.array(z.number().int().positive()).max(10000),
  })
}

export type AsyncMediaRetryValues = typeof defaultAsyncMediaRetry
