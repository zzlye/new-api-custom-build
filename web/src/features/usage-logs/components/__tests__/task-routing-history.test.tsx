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
import { act, render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { afterEach, describe, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { TaskRoutingHistory } from '../task-routing-history'

afterEach(async () => {
  await act(async () => {
    await i18next.changeLanguage('en')
  })
})

describe('媒体任务路由详情', () => {
  it('中文翻译注册在正确命名空间，切换语言后更新决策文案', async () => {
    i18next.addResourceBundle('zhCN', 'translation', zh.translation)
    await i18next.changeLanguage('zhCN')
    render(
      <TaskRoutingHistory
        events={[
          {
            attempt: 1,
            channel_id: 12,
            status: 500,
            elapsed_ms: 1234,
            decision: {
              action: 'retry',
              reason: 'retry_status_matched',
              source: 'async_media',
            },
          },
        ]}
      />
    )
    expect(screen.getByText('媒体路由记录')).toBeVisible()
    expect(screen.getByText('换渠道重试')).toBeVisible()
    expect(screen.getByText('1,234 ms')).toBeVisible()
    expect(i18next.t('Media task retries')).toBe('媒体任务重试')
    expect(i18next.t('Retry HTTP error codes')).toBe('触发重试的 HTTP 错误码')
    await act(async () => {
      await i18next.changeLanguage('en')
    })
    expect(screen.getByText('Media routing history')).toBeVisible()
  })
  it('旧任务没有路由事件时不出现空表格', () => {
    render(<TaskRoutingHistory />)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
  it('显示渠道原始状态与换渠道决策，长记录在固定区域滚动', () => {
    render(
      <TaskRoutingHistory
        events={[
          {
            attempt: 1,
            channel_id: 12,
            status: 500,
            elapsed_ms: 98,
            decision: {
              action: 'retry',
              reason: 'retry_status_matched',
              source: 'async_media',
            },
          },
        ]}
      />
    )
    expect(screen.getByRole('cell', { name: '#12' })).toBeVisible()
    expect(screen.getByRole('cell', { name: '500' })).toBeVisible()
    expect(screen.getByText('Switch channel')).toBeVisible()
    expect(screen.getByRole('table').closest('.max-h-80')).toHaveClass(
      'overflow-auto'
    )
  })
})
