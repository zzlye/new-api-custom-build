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
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

// 日志百分比和看板共用同一开关；根用户不受公开开关影响。
export function useCacheStatsVisible() {
  const role = useAuthStore((state) => state.auth.user?.role)
  const isPublic = useSystemConfigStore(
    (state) => state.config.cacheStatsPublic
  )
  return (
    role === ROLE.SUPER_ADMIN ||
    (role != null && role >= ROLE.USER && isPublic === true)
  )
}
