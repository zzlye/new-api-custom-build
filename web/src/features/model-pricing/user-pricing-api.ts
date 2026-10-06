import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { ModelPricingChange, ModelPricingConfig } from './api'

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
