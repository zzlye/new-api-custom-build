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
  topup_scope: 'lifetime' | 'period'
  topup_start: number
  topup_end: number
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
  eligible_topup_quota: number
  created_at: number
  started_at: number
  ended_at: number
}

// 用户端仅使用参与资格和个人次数，奖池与发奖配置只由 root 管理接口返回。
export type LotteryParticipantActivity = Pick<
  LotteryActivity,
  | 'id'
  | 'name'
  | 'status'
  | 'min_topup_quota'
  | 'topup_scope'
  | 'topup_start'
  | 'topup_end'
  | 'per_user_limit'
  | 'used_draws'
  | 'eligible_topup_quota'
  | 'created_at'
  | 'started_at'
  | 'ended_at'
>

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

export type LotteryWinner = { id: number; username: string; quota: number }
