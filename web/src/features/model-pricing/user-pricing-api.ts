import type { User } from '@/features/users/types'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { ModelPricingChange, ModelPricingConfig } from './api'

export type PricingUser = Pick<User, 'id' | 'username' | 'display_name'> & {
  model_count?: number
}

export async function getConfiguredPricingUsers(keyword: string, page: number) {
  const params = new URLSearchParams({
    keyword,
    p: String(page),
    page_size: '20',
  })
  const response = await api.get(`/api/option/user_model_pricing?${params}`)
  requireServerSuccess(response.data)
  return response.data.data as { items: PricingUser[]; total: number }
}

export async function getUserModelPricing(
  userId: number
): Promise<ModelPricingConfig> {
  const response = await api.get(`/api/option/user_model_pricing/${userId}`)
  requireServerSuccess(response.data)
  return response.data.data
}

export async function saveUserModelPricing(
  userId: number,
  changes: ModelPricingChange[]
) {
  const response = await api.patch(`/api/option/user_model_pricing/${userId}`, {
    changes,
  })
  requireServerSuccess(response.data)
}
