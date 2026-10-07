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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import type { TaskLog } from '../../types'
import { createChannelColumn } from '../columns/column-helpers'
import { useTaskLogsColumns } from '../columns/task-logs-columns'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider, useUsageLogsContext } from '../usage-logs-provider'

const log: TaskLog = {
  id: 1,
  user_id: 31,
  platform: 'openai',
  task_id: 'async_channel_test',
  action: 'IMAGE',
  channel_id: 69,
  group: 'default',
  quota: 1,
  submit_time: 1,
  status: 'SUCCESS',
  is_async: true,
}

interface FixtureProps {
  name?: string
  id?: number
  admin?: boolean
  mobile?: boolean
  legacy?: boolean
}

function Fixture(props: FixtureProps) {
  const columns = useTaskLogsColumns(props.admin ?? true, false)
  const context = useUsageLogsContext()
  // 使用真实表格列和手机卡，避免测试复制一份展示逻辑。
  const table = useReactTable({
    data: [{ ...log, channel_id: props.id ?? 69, channel_name: props.name }],
    columns: props.legacy
      ? [createChannelColumn<TaskLog>({ headerLabel: 'Channel' })]
      : columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'channel_id')
  return (
    <>
      <button type='button' onClick={() => context.setSensitiveVisible(false)}>
        Hide sensitive data
      </button>
      {props.mobile ? (
        <UsageLogsMobileList table={table} logCategory='task' />
      ) : (
        <div aria-label='Channel cell'>
          {cell && flexRender(cell.column.columnDef.cell, cell.getContext())}
        </div>
      )}
    </>
  )
}

function renderChannel(props: FixtureProps = {}) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UsageLogsProvider>
        <Fixture {...props} />
      </UsageLogsProvider>
    </QueryClientProvider>
  )
}

it('管理员看到名称和编号，点击仍只复制渠道编号', async () => {
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  renderChannel({ name: '图片主渠道' })
  expect(screen.getByText('图片主渠道')).toBeVisible()
  await user.click(screen.getByText('#69'))
  expect(copy).toHaveBeenCalledWith('69')
})

it.each([undefined, '', '   '])(
  '名称缺失或为空 %s 时只显示编号，兼容旧接口和已删除渠道',
  (name) => {
    renderChannel({ name })
    expect(screen.getByLabelText('Channel cell')).toHaveTextContent(/^#69$/)
  }
)

it('尚未分配渠道时保持占位符', () => {
  renderChannel({ id: 0, name: '不应展示的名称' })
  expect(screen.getByLabelText('Channel cell')).toHaveTextContent(/^-$/)
})

it('普通用户没有渠道列，即使响应携带名称也不显示', () => {
  renderChannel({ admin: false, name: '图片主渠道' })
  expect(screen.getByLabelText('Channel cell')).toBeEmptyDOMElement()
})

it('隐藏敏感信息后名称和完整标题同步隐藏，编号仍可见', async () => {
  const user = userEvent.setup()
  renderChannel({ name: '图片主渠道' })
  await user.click(screen.getByRole('button', { name: 'Hide sensitive data' }))
  expect(screen.queryByText('图片主渠道')).not.toBeInTheDocument()
  expect(screen.queryByTitle(/图片主渠道/)).not.toBeInTheDocument()
  expect(screen.getByText('#69')).toBeVisible()
  expect(screen.getByText('••••')).toBeVisible()
})

it('超长名称截断并保留完整标题和不压缩的编号', () => {
  const name = '生产环境图片渠道'.repeat(30)
  renderChannel({ name })
  expect(screen.getByText(name)).toHaveClass('truncate')
  expect(screen.getByTitle(`${name} #69`)).toHaveClass('max-w-[180px]')
  expect(screen.getByText('#69')).toHaveClass('shrink-0')
})

it('手机任务卡同时显示名称和编号，不会被摘要折叠隐藏', () => {
  renderChannel({ name: '图片主渠道', mobile: true })
  expect(screen.getByText('图片主渠道')).toBeVisible()
  expect(screen.getByText('#69')).toBeVisible()
})

it('未启用名称的其他日志列保持原来的编号显示', () => {
  renderChannel({ name: '图片主渠道', legacy: true })
  expect(screen.getByLabelText('Channel cell')).toHaveTextContent(/^#69$/)
})
