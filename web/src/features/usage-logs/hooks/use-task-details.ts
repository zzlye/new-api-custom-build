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
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

import type { TaskDetails } from '../types'

// 详情只在主动打开或点击刷新时读取；后台生成不依赖页面是否继续查询。
export function useTaskDetails(taskId: string, enabled = true) {
  return useQuery({
    queryKey: ['async-task-details', taskId],
    queryFn: async ({ signal }) => {
      const response = await api.get<{
        success: boolean
        data: TaskDetails
        message?: string
      }>(`/api/task/${encodeURIComponent(taskId)}/details`, {
        signal,
        disableDuplicate: true,
        skipErrorHandler: true,
      })
      if (!response.data.success) {
        throw new Error(response.data.message || 'Failed to load task details')
      }
      return response.data.data
    },
    refetchInterval: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchOnMount: 'always',
    staleTime: 0,
    retry: false,
    enabled,
  })
}
