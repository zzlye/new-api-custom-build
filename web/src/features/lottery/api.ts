import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  LotteryActivity,
  LotteryDraw,
  LotteryPage,
  LotteryParticipantActivity,
  LotteryRules,
  LotteryWinner,
} from './types'

type Response<T> = { success: boolean; message?: string; data: T }
type ActivityList<T> = LotteryPage<T> & { paid_topup_quota: number }

export function getLotteryActivities(
  root: true,
  page: number
): Promise<ActivityList<LotteryActivity>>
export function getLotteryActivities(
  root: false,
  page: number
): Promise<ActivityList<LotteryParticipantActivity>>
export async function getLotteryActivities(root: boolean, page: number) {
  const response = await api.get<
    Response<ActivityList<LotteryActivity | LotteryParticipantActivity>>
  >(root ? '/api/lottery' : '/api/user/lottery', {
    params: { p: page, page_size: 10 },
  })
  return requireServerSuccess(response.data).data
}

export async function saveLotteryActivity(
  id: number | undefined,
  rules: LotteryRules
) {
  const response = id
    ? await api.put<Response<LotteryActivity>>(`/api/lottery/${id}`, rules)
    : await api.post<Response<LotteryActivity>>('/api/lottery', rules)
  return requireServerSuccess(response.data).data
}

export async function changeLotteryState(
  id: number,
  action: 'start' | 'pause' | 'end'
) {
  const response = await api.post<Response<LotteryActivity>>(
    `/api/lottery/${id}/state`,
    { action }
  )
  return requireServerSuccess(response.data).data
}

export async function drawLottery(id: number, requestId: string) {
  const response = await api.post<Response<LotteryDraw>>(
    `/api/user/lottery/${id}/draw`,
    { request_id: requestId }
  )
  return requireServerSuccess(response.data).data
}

export async function getLotteryDraws(root: boolean, id: number, page: number) {
  const base = root ? '/api/lottery' : '/api/user/lottery'
  const response = await api.get<Response<LotteryPage<LotteryDraw>>>(
    `${base}/${id}/draws`,
    { params: { p: page, page_size: 20 } }
  )
  return requireServerSuccess(response.data).data
}

export async function deleteLotteryActivity(id: number) {
  const response = await api.delete<Response<null>>(`/api/lottery/${id}`)
  requireServerSuccess(response.data)
}

export async function getLotteryWinners(id: number) {
  const response = await api.get<Response<LotteryWinner[]>>(
    `/api/user/lottery/${id}/winners`
  )
  return requireServerSuccess(response.data).data
}
