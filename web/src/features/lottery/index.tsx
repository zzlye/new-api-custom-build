import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Gift, History, RefreshCw } from 'lucide-react'
import { nanoid } from 'nanoid'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useSystemConfig } from '@/hooks/use-system-config'
import { toIntlLocale } from '@/i18n/languages'
import { getServerErrorSources } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { drawLottery, getLotteryActivities } from './api'
import { LotteryHistory } from './lottery-history'
import { lotteryMoney } from './reward-format'
import { LOTTERY_MODES, LOTTERY_STATUSES, type LotteryActivity } from './types'

export function LotteryPage() {
  const { t, i18n } = useTranslation()
  useSystemConfig()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [history, setHistory] = useState<LotteryActivity | null>(null)
  const userId = useAuthStore((s) => s.auth.user?.id)
  // 请求失败时保留编号；再次点击先取回同一结果，避免响应丢失导致重复抽奖。
  const requests = useRef(new Map<number, string>())
  const storageKey = (id: number) => `lottery-request:${userId}:${id}`
  const pendingRequest = (id: number) => {
    const memory = requests.current.get(id)
    if (memory) return memory
    try {
      return sessionStorage.getItem(storageKey(id)) ?? undefined
    } catch {
      return undefined
    }
  }
  const clearRequest = (id: number) => {
    requests.current.delete(id)
    try {
      sessionStorage.removeItem(storageKey(id))
    } catch {
      /* 隐私模式下仍保留内存幂等保护。 */
    }
  }
  const query = useQuery({
    queryKey: ['lottery', 'user', page],
    queryFn: () => getLotteryActivities(false, page),
  })
  const draw = useMutation({
    mutationFn: (id: number) => {
      const key = pendingRequest(id) ?? nanoid()
      requests.current.set(id, key)
      try {
        sessionStorage.setItem(storageKey(id), key)
      } catch {
        /* 存储不可用时沿用内存编号。 */
      }
      return drawLottery(id, key)
    },
    retry: false,
    onSuccess: (result) => {
      clearRequest(result.activity_id)
      toast.success(
        t('You received {{amount}} in bonus credit', {
          amount: lotteryMoney(result.quota, locale),
        })
      )
      void client.invalidateQueries({ queryKey: ['lottery'] })
    },
    onSettled: (_result, error, id) => {
      // 明确业务拒绝没有发奖；网络中断的请求保留编号，刷新后也可取回原结果。
      if (
        getServerErrorSources(error).some((source) => source.success === false)
      ) {
        clearRequest(id)
      }
      void client.invalidateQueries({ queryKey: ['lottery', 'user'] })
    },
  })
  return (
    <SectionPageLayout glassHeader>
      <SectionPageLayout.Title>{t('Lucky draw')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw aria-hidden='true' />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {query.isError ? (
            <ErrorState onRetry={() => void query.refetch()} />
          ) : null}
          {query.isPending ? <p role='status'>{t('Loading...')}</p> : null}
          {!query.isPending && !query.isError && !query.data?.total ? (
            <EmptyState title={t('No draw activities')} />
          ) : null}
          {query.data?.items.map((activity) => {
            const available = Math.max(
              0,
              activity.per_user_limit - activity.used_draws
            )
            const qualified =
              query.data.paid_topup_quota >= activity.min_topup_quota
            const retrying = !!pendingRequest(activity.id)
            let drawLabel = retrying ? t('Retrieve draw result') : t('Draw now')
            if (draw.isPending && draw.variables === activity.id) {
              drawLabel = t('Drawing...')
            }
            return (
              <div
                key={activity.id}
                className='appearance-glass-surface space-y-3 rounded-lg border p-4'
              >
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <h2 className='min-w-0 font-medium break-words'>
                    {activity.name}
                  </h2>
                  <Badge variant='secondary'>
                    {t(LOTTERY_STATUSES[activity.status])}
                  </Badge>
                </div>
                <dl className='grid grid-cols-2 gap-3 text-sm sm:grid-cols-4 [&_dd]:break-all [&_div]:min-w-0'>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Remaining prize pool')}
                    </dt>
                    <dd>{lotteryMoney(activity.remaining_quota, locale)}</dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Cumulative top-up required')}
                    </dt>
                    <dd>{lotteryMoney(activity.min_topup_quota, locale)}</dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Your cumulative top-ups')}
                    </dt>
                    <dd>{lotteryMoney(query.data.paid_topup_quota, locale)}</dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('Draws remaining')}
                    </dt>
                    <dd>{available}</dd>
                  </div>
                </dl>
                <div className='flex flex-wrap items-center justify-between gap-3'>
                  <p className='text-muted-foreground text-sm'>
                    {t(LOTTERY_MODES[activity.mode])}
                    {activity.mode.endsWith('_percent')
                      ? ` · ${activity.min_percent / 100}%–${activity.max_percent / 100}%`
                      : ''}
                    {activity.mode === 'fixed'
                      ? ` · ${lotteryMoney(activity.fixed_quota, locale)}`
                      : ''}
                    {activity.mode === 'random_shares'
                      ? ` · ${t('{{count}} shares remaining', { count: activity.remaining_shares })}`
                      : ''}
                  </p>
                  <div className='flex gap-2'>
                    <Button
                      variant='outline'
                      onClick={() => setHistory(activity)}
                    >
                      <History aria-hidden='true' />
                      {t('Draw history')}
                    </Button>
                    <Button
                      disabled={
                        draw.isPending ||
                        (!retrying &&
                          (activity.status !== 'active' ||
                            !qualified ||
                            available === 0))
                      }
                      onClick={() => draw.mutate(activity.id)}
                    >
                      <Gift aria-hidden='true' />
                      {drawLabel}
                    </Button>
                  </div>
                </div>
              </div>
            )
          })}
          {(query.data?.total ?? 0) > 10 ? (
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
                disabled={
                  page * 10 >= (query.data?.total ?? 0) || query.isFetching
                }
                onClick={() => setPage(page + 1)}
              >
                {t('Next page')}
              </Button>
            </div>
          ) : null}
        </div>
        {history ? (
          <LotteryHistory
            key={history.id}
            activity={history}
            onClose={() => setHistory(null)}
          />
        ) : null}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
