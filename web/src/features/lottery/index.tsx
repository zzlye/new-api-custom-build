import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { nanoid } from 'nanoid'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Button } from '@/components/ui/button'

import '@/styles/lottery.css'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useSystemConfig } from '@/hooks/use-system-config'
import { toIntlLocale } from '@/i18n/languages'
import { getServerErrorSources } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { drawLottery, getLotteryActivities } from './api'
import { LotteryHistory } from './lottery-history'
import { LotteryStage } from './lottery-stage'
import { LotteryWinners } from './lottery-winners'
import { lotteryMoney } from './reward-format'
import type { LotteryParticipantActivity } from './types'

export function LotteryPage() {
  const { t, i18n } = useTranslation()
  useSystemConfig()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<number | null>(null)
  const [history, setHistory] = useState<LotteryParticipantActivity | null>(
    null
  )
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
  const activity =
    query.data?.items.find((item) => item.id === selected) ??
    query.data?.items[0]
  return (
    <SectionPageLayout glassHeader>
      <SectionPageLayout.Title>{t('Lucky draw')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          disabled={query.isFetching}
          onClick={() =>
            void client.invalidateQueries({ queryKey: ['lottery'] })
          }
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
          {activity && !query.isError ? (
            <>
              {(query.data?.items.length ?? 0) > 1 ? (
                <div className='appearance-glass-surface ml-auto w-full rounded-xl border p-1 sm:w-72'>
                  <Select
                    value={activity.id}
                    onValueChange={(id) => setSelected(id)}
                  >
                    <SelectTrigger
                      aria-label={t('Choose activity')}
                      className='w-full'
                    >
                      <SelectValue>{activity.name}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {query.data?.items.map((item) => (
                          <SelectItem key={item.id} value={item.id}>
                            {item.name}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              ) : null}
              <LotteryWinners key={activity.id} activityId={activity.id} />
              <LotteryStage
                activity={activity}
                pending={draw.isPending}
                retrying={!!pendingRequest(activity.id)}
                onDraw={() => draw.mutate(activity.id)}
                onHistory={() => setHistory(activity)}
              />
            </>
          ) : null}
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
