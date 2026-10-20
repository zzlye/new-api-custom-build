import { Gift, History, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { lotteryPeriod } from './lottery-time'
import { lotteryMoney } from './reward-format'
import { LOTTERY_STATUSES, type LotteryParticipantActivity } from './types'

export function LotteryStage(props: {
  activity: LotteryParticipantActivity
  pending: boolean
  retrying: boolean
  onDraw: () => void
  onHistory: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const a = props.activity
  const available = Math.max(0, a.per_user_limit - a.used_draws)
  const qualified = a.eligible_topup_quota >= a.min_topup_quota
  const period = a.topup_scope === 'period'
  let drawLabel = props.retrying ? t('Retrieve draw result') : t('Draw now')
  if (props.pending) drawLabel = t('Drawing...')

  return (
    <section
      className='appearance-glass-surface lottery-stage overflow-hidden rounded-3xl border'
      aria-label={a.name}
    >
      <div className='relative flex flex-wrap items-center justify-between gap-3 px-6 pt-6 sm:px-9'>
        <h2 className='min-w-0 text-xl font-semibold break-words sm:text-2xl'>
          {a.name}
        </h2>
        <Badge variant='secondary'>{t(LOTTERY_STATUSES[a.status])}</Badge>
      </div>
      <div className='relative grid min-w-0 gap-6 p-6 sm:p-9 lg:min-h-[420px] lg:grid-cols-[1.35fr_1fr] lg:gap-12'>
        <div className='flex min-w-0 flex-col items-center justify-center py-4 text-center sm:py-5'>
          <div
            className='lottery-gift border-primary/20 bg-primary/10 text-primary flex size-32 items-center justify-center rounded-3xl border sm:size-44 dark:text-sky-300'
            aria-hidden='true'
          >
            <Gift className='size-16 sm:size-24' strokeWidth={1.3} />
            <Sparkles className='lottery-spark size-6' />
          </div>
        </div>
        <div className='bg-background/30 flex min-w-0 flex-col justify-center rounded-2xl border p-5 sm:p-7'>
          <div className='flex items-center justify-between gap-4'>
            <h3 className='font-medium'>{t('Your draw chances')}</h3>
            <Button variant='ghost' size='sm' onClick={props.onHistory}>
              <History aria-hidden='true' />
              {t('Draw history')}
            </Button>
          </div>
          <div className='my-5 flex items-baseline gap-3'>
            <span className='text-5xl font-semibold tabular-nums'>
              {formatNumber(available, locale)}
            </span>
            <span className='text-muted-foreground text-sm'>
              {t('Draws remaining')}
            </span>
          </div>
          <Separator />
          <dl className='mt-5 space-y-3 text-sm'>
            <div className='flex flex-wrap justify-between gap-2'>
              <dt className='text-muted-foreground'>
                {t(
                  period
                    ? 'Period top-up required'
                    : 'Cumulative top-up required'
                )}
              </dt>
              <dd className='font-medium break-all'>
                {lotteryMoney(a.min_topup_quota, locale)}
              </dd>
            </div>
            <div className='flex flex-wrap justify-between gap-2'>
              <dt className='text-muted-foreground'>
                {t(
                  period
                    ? 'Your top-ups in this period'
                    : 'Your cumulative top-ups'
                )}
              </dt>
              <dd className='font-medium break-all'>
                {lotteryMoney(a.eligible_topup_quota, locale)}
              </dd>
            </div>
          </dl>
          {period ? (
            <p className='text-muted-foreground mt-3 text-xs leading-relaxed'>
              {t('Beijing time')}
              <br />
              {lotteryPeriod(a.topup_start, a.topup_end)}
            </p>
          ) : null}
          <Progress
            className='mt-4'
            aria-label={t('Top-up eligibility')}
            value={
              a.min_topup_quota
                ? Math.min(
                    100,
                    (a.eligible_topup_quota / a.min_topup_quota) * 100
                  )
                : 100
            }
          />
          <p className='text-muted-foreground mt-3 text-sm'>
            {t(
              qualified
                ? 'Eligible to participate'
                : 'Top-up requirement not met'
            )}
          </p>
          <Button
            className='mt-6 h-12 w-full text-base'
            disabled={
              props.pending ||
              (!props.retrying &&
                (a.status !== 'active' || !qualified || available === 0))
            }
            onClick={props.onDraw}
          >
            <Gift aria-hidden='true' />
            {drawLabel}
          </Button>
        </div>
      </div>
    </section>
  )
}
