import { z } from 'zod'

export const manualCreditSchema = z.object({
  actual_money: z
    .string()
    .trim()
    .regex(
      /^[0-9]{1,6}(\.[0-9]{1,2})?$/,
      'Enter a valid amount with up to two decimals.'
    )
    .refine(
      (value) => Number(value) > 0 && Number(value) <= 100001,
      'Amount must be greater than zero and no more than 100001.'
    ),
  provider_trade_no: z
    .string()
    .trim()
    .regex(
      /^[0-9]{8,64}$/,
      'Enter an Alipay transaction number of 8 to 64 digits.'
    ),
  reason: z
    .string()
    .trim()
    .min(1, 'Enter the reason for manual credit.')
    .max(500, 'Reason cannot exceed 500 characters.'),
})

export type ManualCreditValues = z.infer<typeof manualCreditSchema>
