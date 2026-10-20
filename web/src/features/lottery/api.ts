import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  LotteryActivity,
  LotteryDraw,
  LotteryPage,
  LotteryRules,
} from './types'

type Response<T> = { success: boolean; message?: string; data: T }

export async function getLotteryActivities(root: boolean, page: number) {
  const response = await api.get<
    Response<LotteryPage<LotteryActivity> & { paid_topup_quota: number }>
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
