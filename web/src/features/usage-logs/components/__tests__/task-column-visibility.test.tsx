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
import { expect, it } from 'vitest'

import type { TaskLog } from '../../types'
import { useTaskLogsColumns } from '../columns/task-logs-columns'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider } from '../usage-logs-provider'

function Fixture(props: { mobile: boolean; async: boolean; admin: boolean }) {
  const columns = useTaskLogsColumns(props.admin, props.admin)
  const log: TaskLog = {
    id: 1,
    user_id: 31,
    platform: 'internal',
    task_id: 'task-column-test',
    action: 'IMAGE',
    channel_id: 69,
    channel_name: '图片主渠道',
    group: 'default',
    quota: 1,
    submit_time: 1,
    status: 'SUCCESS',
    is_async: props.async,
  }
  // 桌面与手机均使用实际任务列，防止只隐藏标题而留下空单元格。
  const table = useReactTable({
    data: [log],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  if (props.mobile) {
    return <UsageLogsMobileList table={table} logCategory='task' />
  }
  return (
    <table>
      <thead>
        {table.getHeaderGroups().map((group) => (
          <tr key={group.id}>
            {group.headers.map((header) => (
              <th key={header.id}>
                {flexRender(
                  header.column.columnDef.header,
                  header.getContext()
                )}
              </th>
            ))}
          </tr>
        ))}
      </thead>
      <tbody>
        {table.getRowModel().rows.map((row) => (
          <tr key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <td key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

it.each([
  { mobile: false, async: false, admin: false },
  { mobile: false, async: true, admin: true },
  { mobile: true, async: false, admin: true },
  { mobile: true, async: true, admin: false },
])(
  'mobile=$mobile async=$async admin=$admin 时移除制品入口并保留任务详情',
  (props) => {
    render(
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
    expect(screen.queryByText('Artifacts')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Artifacts' })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /View.*details/i })).toBeVisible()
    if (props.admin) expect(screen.getByText('图片主渠道')).toBeVisible()
  }
)
