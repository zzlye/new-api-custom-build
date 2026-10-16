import { createFileRoute, redirect } from '@tanstack/react-router'

import { OrderLogs } from '@/features/order-logs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/order-logs/')({
  beforeLoad: () => {
    if ((useAuthStore.getState().auth.user?.role || 0) < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: OrderLogs,
})
