import { createFileRoute } from '@tanstack/react-router'

import { LotteryPage } from '@/features/lottery'

export const Route = createFileRoute('/_authenticated/lottery/')({
  component: LotteryPage,
})
