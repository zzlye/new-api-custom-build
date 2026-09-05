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
import { z } from 'zod'

// 前后端采用相同的整小时范围，避免空值、无限期和小数导致意外保留文件。
export const mediaRetentionSchema = z.object({
  hours: z
    .number({ error: 'Enter a whole number from 1 to 168' })
    .int('Enter a whole number from 1 to 168')
    .min(1, 'Enter a whole number from 1 to 168')
    .max(168, 'Enter a whole number from 1 to 168'),
})
export type MediaRetentionValues = z.infer<typeof mediaRetentionSchema>
