import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampToDate } from '@/lib/format'

import { getLotteryDraws } from './api'
import { lotteryMoney } from './reward-format'
import type { LotteryActivity } from './types'

export function LotteryHistory(props: {
  activity: Pick<LotteryActivity, 'id' | 'name'>
  root?: boolean
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['lottery', 'draws', !!props.root, props.activity.id, page],
    queryFn: () => getLotteryDraws(!!props.root, props.activity.id, page),
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={`${props.activity.name} · ${t('Draw history')}`}
    >
      {query.isError ? (
        <ErrorState onRetry={() => void query.refetch()} />
      ) : (
        <StaticDataTable
          data={query.data?.items ?? []}
          getRowKey={(row) => row.id}
          emptyContent={
            query.isPending ? t('Loading...') : t('No draw records')
          }
          columns={[
            { id: 'id', header: t('Record ID'), cell: (row) => row.id },
            ...(props.root
              ? [
                  {
                    id: 'user',
                    header: t('User'),
                    cell: (row: { user_id: number; username?: string }) =>
                      `${row.username || '-'} (#${row.user_id})`,
                  },
                ]
              : []),
            {
              id: 'reward',
              header: t('Bonus reward'),
              cell: (row) => lotteryMoney(row.quota, locale),
            },
            {
              id: 'time',
              header: t('Time'),
              cell: (row) => formatTimestampToDate(row.created_at),
            },
            ...(props.root
              ? [
                  {
                    id: 'ip',
                    header: 'IP',
                    cell: (row: { ip?: string }) => row.ip || '-',
                  },
                ]
              : []),
          ]}
        />
      )}
      <div className='flex justify-end gap-2'>
        <Button
          variant='outline'
          disabled={page === 1 || query.isFetching}
          onClick={() => setPage(page - 1)}
        >
          {t('Previous page')}
        </Button>
        <Button
          variant='outline'
          disabled={page * 20 >= (query.data?.total ?? 0) || query.isFetching}
          onClick={() => setPage(page + 1)}
        >
          {t('Next page')}
        </Button>
      </div>
    </Dialog>
  )
}
