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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { ModelCard } from '../components/model-card'
import { ModelDetailsContent } from '../components/model-details'
import { ModelPriceCell } from '../components/model-price-cell'
import type { PricingModel } from '../types'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

const expression =
  'param("resolution") == "1080p" ? 0.93 : param("resolution") == "720p" ? 0.48 : 0.27'
let client: QueryClient
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.spyOn(api, 'get').mockResolvedValue({ data: { data: { groups: [] } } })
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
})
afterEach(() => {
  client.clear()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

describe('分辨率价格展示', () => {
  it.each(['per_second', 'per_request'])(
    '%s 模型详情展示三档基础价格与分组倍率后的价格',
    (mode) => {
      const model: PricingModel = {
        id: 1,
        model_name: 'wan3.0',
        quota_type: 1,
        model_ratio: 0,
        completion_ratio: 0,
        model_price: 0.48,
        billing_mode: mode,
        billing_expr: expression,
        enable_groups: ['premium', 'free'],
      }
      render(
        <QueryClientProvider client={client}>
          <ModelDetailsContent
            model={model}
            groupRatio={{ premium: 2, free: 0 }}
            usableGroup={{
              premium: { desc: '', ratio: 2 },
              free: { desc: '', ratio: 0 },
            }}
            endpointMap={{}}
            autoGroups={[]}
            priceRate={1}
            usdExchangeRate={1}
            tokenUnit='M'
          />
        </QueryClientProvider>
      )
      // 使用真实详情页，保护三档价格而不是隐藏的720p兜底价格。
      for (const resolution of ['480p', '720p', '1080p']) {
        expect(screen.getAllByText(resolution).length).toBeGreaterThan(0)
      }
      for (const price of ['0.27', '0.48', '0.93', '0.54', '0.96', '1.86']) {
        expect(
          screen.getAllByText(new RegExp(`\\$${price.replace('.', '\\.')}`))
            .length
        ).toBeGreaterThan(0)
      }
      expect(screen.getByText('0x')).toBeInTheDocument()
      const freeRow = screen.getByText('0x').closest('tr')
      if (!freeRow) throw new Error('免费分组价格必须显示在分组表格行中')
      expect(within(freeRow).getAllByText(/\$0(?:\s|$)/)).toHaveLength(3)
    }
  )

  it.each(['per_second', 'per_request'])(
    '%s 模型卡片及价格列展示所选分组的三档价格',
    (mode) => {
      const model: PricingModel = {
        id: 1,
        model_name: 'wan3.0',
        quota_type: 1,
        model_ratio: 0,
        completion_ratio: 0,
        model_price: 0.48,
        billing_mode: mode,
        billing_expr: expression,
        enable_groups: ['premium'],
        group_ratio: { premium: 2 },
      }
      render(
        <QueryClientProvider client={client}>
          <div data-testid='card'>
            <ModelCard
              model={model}
              onClick={vi.fn()}
              selectedGroup='premium'
            />
          </div>
          <div data-testid='cell'>
            <ModelPriceCell
              model={model}
              options={{ selectedGroup: 'premium' }}
            />
          </div>
        </QueryClientProvider>
      )
      for (const id of ['card', 'cell']) {
        const content = within(screen.getByTestId(id))
        for (const resolution of ['480p', '720p', '1080p']) {
          expect(content.getByText(resolution)).toBeInTheDocument()
        }
        expect(content.getByText(/0\.54/)).toBeInTheDocument()
        expect(content.getByText(/1\.86/)).toBeInTheDocument()
      }
    }
  )
})
