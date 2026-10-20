import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useSystemConfig } from '@/hooks/use-system-config'
import { toIntlLocale } from '@/i18n/languages'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  changeLotteryState,
  deleteLotteryActivity,
  getLotteryActivities,
} from './api'
import { LotteryForm } from './lottery-form'
import { LotteryHistory } from './lottery-history'
import { lotteryPeriod } from './lottery-time'
import { lotteryMoney } from './reward-format'
import { LOTTERY_MODES, LOTTERY_STATUSES, type LotteryActivity } from './types'

export function LotterySettings() {
  const { t, i18n } = useTranslation()
  useSystemConfig()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [editor, setEditor] = useState<LotteryActivity | 'new' | null>(null)
  const [history, setHistory] = useState<LotteryActivity | null>(null)
  const [ending, setEnding] = useState<LotteryActivity | null>(null)
  const [deleting, setDeleting] = useState<LotteryActivity | null>(null)
  const query = useQuery({
    queryKey: ['lottery', 'admin', page],
    queryFn: () => getLotteryActivities(true, page),
    enabled: root,
  })
  const update = useMutation({
    mutationFn: (args: { id: number; action: 'start' | 'pause' | 'end' }) =>
      changeLotteryState(args.id, args.action),
    onSuccess: () => {
      toast.success(t('Saved successfully'))
      setEnding(null)
      void client.invalidateQueries({ queryKey: ['lottery'] })
    },
  })
  const remove = useMutation({
    mutationFn: deleteLotteryActivity,
    onSuccess: () => {
      toast.success(t('Deleted successfully'))
      setDeleting(null)
      setPage(1)
      void client.invalidateQueries({ queryKey: ['lottery'] })
    },
  })
  if (!root) return null
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap justify-end gap-2'>
        <Button
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
        <Button onClick={() => setEditor('new')}>
          {t('Create draw activity')}
        </Button>
      </div>
      {query.isError ? (
        <ErrorState onRetry={() => void query.refetch()} />
      ) : (
        <StaticDataTable
          data={query.data?.items ?? []}
          getRowKey={(row) => row.id}
          emptyContent={
            query.isPending ? t('Loading...') : t('No draw activities')
          }
          columns={[
            { id: 'name', header: t('Activity name'), cell: (a) => a.name },
            {
              id: 'status',
              header: t('Status'),
              cell: (a) => (
                <Badge variant='secondary'>
                  {t(LOTTERY_STATUSES[a.status])}
                </Badge>
              ),
            },
            {
              id: 'mode',
              header: t('Reward mode'),
              cell: (a) => (
                <div>
                  {t(LOTTERY_MODES[a.mode])}
                  {a.mode.endsWith('_percent') ? (
                    <div>
                      {a.min_percent / 100}%–{a.max_percent / 100}%
                    </div>
                  ) : null}
                  {a.mode === 'fixed' ? (
                    <div>{lotteryMoney(a.fixed_quota, locale)}</div>
                  ) : null}
                </div>
              ),
            },
            {
              id: 'pool',
              header: t('Remaining / total pool'),
              cell: (a) =>
                `${lotteryMoney(a.remaining_quota, locale)} / ${lotteryMoney(a.total_quota, locale)}`,
            },
            {
              id: 'threshold',
              header: t('Top-up eligibility'),
              cell: (a) => (
                <div className='space-y-1'>
                  <div>
                    {t(
                      a.topup_scope === 'period'
                        ? 'Period top-up required'
                        : 'Cumulative top-up required'
                    )}
                  </div>
                  <div>{lotteryMoney(a.min_topup_quota, locale)}</div>
                  {a.topup_scope === 'period' ? (
                    <div className='text-muted-foreground max-w-64 text-xs'>
                      {t('Beijing time')}
                      <br />
                      {lotteryPeriod(a.topup_start, a.topup_end)}
                    </div>
                  ) : null}
                </div>
              ),
            },
            {
              id: 'limit',
              header: t('Draws per user'),
              cell: (a) => a.per_user_limit,
            },
            {
              id: 'draws',
              header: t('Completed draws'),
              cell: (a) => a.draw_count,
            },
            {
              id: 'shares',
              header: t('Remaining / total shares'),
              cell: (a) =>
                a.mode === 'random_shares'
                  ? `${a.remaining_shares} / ${a.shares}`
                  : '-',
            },
            {
              id: 'actions',
              header: t('Actions'),
              cell: (a) => (
                <div className='flex flex-wrap gap-1'>
                  {a.status === 'draft' ? (
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => setEditor(a)}
                    >
                      {t('Edit')}
                    </Button>
                  ) : null}
                  {a.status === 'draft' || a.status === 'paused' ? (
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={update.isPending}
                      onClick={() =>
                        update.mutate({ id: a.id, action: 'start' })
                      }
                    >
                      {t(a.status === 'paused' ? 'Resume' : 'Start')}
                    </Button>
                  ) : null}
                  {a.status === 'active' ? (
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={update.isPending}
                      onClick={() =>
                        update.mutate({ id: a.id, action: 'pause' })
                      }
                    >
                      {t('Pause')}
                    </Button>
                  ) : null}
                  {['draft', 'active', 'paused'].includes(a.status) ? (
                    <Button
                      size='sm'
                      variant='ghost'
                      disabled={update.isPending}
                      onClick={() => setEnding(a)}
                    >
                      {t('End activity')}
                    </Button>
                  ) : null}
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setHistory(a)}
                  >
                    {t('Draw history')}
                  </Button>
                  {['draft', 'ended', 'exhausted'].includes(a.status) ? (
                    <Button
                      size='sm'
                      variant='ghost'
                      disabled={remove.isPending}
                      onClick={() => setDeleting(a)}
                    >
                      {t('Delete')}
                    </Button>
                  ) : null}
                </div>
              ),
            },
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
          disabled={page * 10 >= (query.data?.total ?? 0) || query.isFetching}
          onClick={() => setPage(page + 1)}
        >
          {t('Next page')}
        </Button>
      </div>
      {editor ? (
        <LotteryForm
          key={editor === 'new' ? 'new' : editor.id}
          activity={editor === 'new' ? undefined : editor}
          onClose={() => setEditor(null)}
        />
      ) : null}
      {history ? (
        <LotteryHistory
          key={history.id}
          root
          activity={history}
          onClose={() => setHistory(null)}
        />
      ) : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => {
          if (!open && !remove.isPending) setDeleting(null)
        }}
        title={t('Delete draw activity')}
        desc={t(
          'Delete this activity? Issued rewards and financial records will be kept.'
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={remove.isPending}
        handleConfirm={() => {
          if (deleting) remove.mutate(deleting.id)
        }}
      />
      <ConfirmDialog
        open={!!ending}
        onOpenChange={(open) => {
          if (!open) setEnding(null)
        }}
        title={t('End activity')}
        desc={t('End this activity? It cannot be resumed.')}
        confirmText={t('End activity')}
        isLoading={update.isPending}
        handleConfirm={() => {
          if (ending) update.mutate({ id: ending.id, action: 'end' })
        }}
      />
    </div>
  )
}
