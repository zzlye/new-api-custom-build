export const LOTTERY_MODES = {
  initial_percent: 'Percentage of initial pool',
  remaining_percent: 'Percentage of remaining pool',
  random_shares: 'Random shares',
  fixed: 'Fixed reward',
} as const

export const LOTTERY_STATUSES = {
  draft: 'Draft',
  active: 'In progress',
  paused: 'Paused',
  ended: 'Ended',
  exhausted: 'Pool exhausted',
} as const

export type LotteryRules = {
  name: string
  mode: keyof typeof LOTTERY_MODES
  total_quota: number
  min_topup_quota: number
  per_user_limit: number
  min_percent: number
  max_percent: number
  shares: number
  fixed_quota: number
}

export type LotteryActivity = LotteryRules & {
  id: number
  status: keyof typeof LOTTERY_STATUSES
  remaining_quota: number
  remaining_shares: number
  draw_count: number
  used_draws: number
  created_at: number
  started_at: number
  ended_at: number
}

export type LotteryDraw = {
  id: number
  activity_id: number
  user_id: number
  username?: string
  quota: number
  created_at: number
  ip?: string
}

export type LotteryPage<T> = { items: T[]; total: number }
