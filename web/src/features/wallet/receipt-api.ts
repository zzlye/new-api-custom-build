import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface ReceiptOrder {
  id: number
  trade_no: string
  status: string
  paid_time: number
  receipt: {
    amount_cents: number
    credit_quota: number
    qr_code: string
    confirmed_at: number
    expires_at: number
  }
}

// 始终通过本站会话查询订单，不把私钥或支付宝查账权限交给浏览器。
export async function createReceiptOrder(
  amount: number
): Promise<ReceiptOrder> {
  const response = await api.post('/api/user/alipay-receipt/pay', { amount })
  requireServerSuccess(response.data)
  return response.data.data
}

export async function getReceiptOrder(tradeNo: string): Promise<ReceiptOrder> {
  const response = await api.get(
    `/api/user/alipay-receipt/order/${encodeURIComponent(tradeNo)}`
  )
  requireServerSuccess(response.data)
  return response.data.data
}

export async function updateReceiptOrder(
  tradeNo: string,
  action: 'confirm' | 'close'
): Promise<ReceiptOrder> {
  const response = await api.post(
    `/api/user/alipay-receipt/order/${encodeURIComponent(tradeNo)}/${action}`
  )
  requireServerSuccess(response.data)
  return response.data.data
}
