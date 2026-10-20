import { useQuery } from '@tanstack/react-query'
import { PartyPopper, Pause, Play } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'

import { getLotteryWinners } from './api'
import { lotteryMoney } from './reward-format'

export function LotteryWinners(props: { activityId: number }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [paused, setPaused] = useState(false)
  const query = useQuery({
    queryKey: ['lottery', 'winners', props.activityId],
    queryFn: () => getLotteryWinners(props.activityId),
  })
  const items = query.data ?? []
  return (
    <section
      aria-label={t('Latest winners')}
      className='appearance-glass-surface flex min-w-0 items-center gap-3 overflow-hidden rounded-2xl border px-4 py-3 sm:gap-5 sm:px-6'
    >
      <h3 className='flex shrink-0 items-center gap-2 text-sm font-medium'>
        <PartyPopper
          className='text-primary size-4 dark:text-sky-300'
          aria-hidden='true'
        />
        {t('Latest winners')}
      </h3>
      {query.isError ? (
        <Button variant='ghost' size='sm' onClick={() => void query.refetch()}>
          {t('Retry')}
        </Button>
      ) : null}
      {!query.isError && !items.length ? (
        <p className='text-muted-foreground text-sm'>
          {t(query.isPending ? 'Loading...' : 'No winners yet')}
        </p>
      ) : null}
      {items.length ? (
        <>
          <div
            className='lottery-ticker min-w-0 flex-1 overflow-hidden'
            data-paused={paused}
          >
            <div
              className='lottery-ticker-track flex w-max'
              style={{
                animationDuration: `${Math.max(24, items.length * 5)}s`,
              }}
            >
              {/* 第二份仅用于无缝滚动，读屏只读取真实的一份中奖列表。 */}
              {[0, 1].map((copy) => (
                <ul
                  key={copy}
                  aria-hidden={copy === 1 ? true : undefined}
                  className='lottery-ticker-list flex shrink-0 items-center gap-10 pr-10'
                >
                  {items.map((winner) => (
                    <li
                      key={winner.id}
                      className='flex items-center gap-2 text-sm whitespace-nowrap'
                    >
                      <span className='text-muted-foreground'>
                        {winner.username}
                      </span>
                      <strong className='text-primary font-semibold tabular-nums dark:text-sky-300'>
                        +{lotteryMoney(winner.quota, locale)}
                      </strong>
                    </li>
                  ))}
                </ul>
              ))}
            </div>
          </div>
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t(
              paused ? 'Resume winner scroll' : 'Pause winner scroll'
            )}
            aria-pressed={paused}
            onClick={() => setPaused(!paused)}
          >
            {paused ? (
              <Play aria-hidden='true' />
            ) : (
              <Pause aria-hidden='true' />
            )}
          </Button>
        </>
      ) : null}
    </section>
  )
}
