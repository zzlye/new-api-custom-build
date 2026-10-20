import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useSystemConfig } from '@/hooks/use-system-config'

import { saveLotteryActivity } from './api'
import { lotteryTimeStamp, lotteryTimeValue } from './lottery-time'
import { lotteryCurrency } from './reward-format'
import { LOTTERY_MODES, type LotteryActivity, type LotteryRules } from './types'

const schema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'Please enter an activity name')
    .max(80, 'Activity name must not exceed 80 characters'),
  mode: z.enum([
    'initial_percent',
    'remaining_percent',
    'random_shares',
    'fixed',
  ]),
  scope: z.enum(['lifetime', 'period']),
  start: z.string(),
  end: z.string(),
  total: z
    .number({ error: 'Please enter a valid amount' })
    .positive('Please enter a valid amount'),
  threshold: z
    .number({ error: 'Please enter a valid amount' })
    .min(0, 'Please enter a valid amount'),
  limit: z
    .number({ error: 'Please enter a valid draw count' })
    .int('Please enter a valid draw count')
    .min(1, 'Please enter a valid draw count')
    .max(2147483647, 'Please enter a valid draw count'),
  minimum: z.union([z.number(), z.nan()]),
  maximum: z.union([z.number(), z.nan()]),
  shares: z.union([z.number(), z.nan()]),
  fixed: z.union([z.number(), z.nan()]),
})
type Values = z.infer<typeof schema>

export function LotteryForm(props: {
  activity?: LotteryActivity
  onClose: () => void
}) {
  const { t } = useTranslation()
  useSystemConfig()
  const { factor, unit } = lotteryCurrency()
  const client = useQueryClient()
  const a = props.activity
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: a?.name ?? '',
      mode: a?.mode ?? 'random_shares',
      scope: a?.topup_scope || 'lifetime',
      start: lotteryTimeValue(a?.topup_start ?? 0),
      end: lotteryTimeValue(a?.topup_end ?? 0),
      total: a ? a.total_quota / factor : 100,
      threshold: a ? a.min_topup_quota / factor : 100,
      limit: a?.per_user_limit ?? 1,
      minimum: (a?.min_percent || 100) / 100,
      maximum: (a?.max_percent || 300) / 100,
      shares: a?.shares || 20,
      fixed: a ? (a.fixed_quota || factor) / factor : 1,
    },
  })
  const save = useMutation({
    mutationFn: (rules: LotteryRules) => saveLotteryActivity(a?.id, rules),
    onSuccess: () => {
      toast.success(t('Saved successfully'))
      void client.invalidateQueries({ queryKey: ['lottery'] })
      props.onClose()
    },
  })
  const mode = form.watch('mode')
  const scope = form.watch('scope')
  const submit = form.handleSubmit((values) => {
    const start = scope === 'period' ? lotteryTimeStamp(values.start) : 0
    const end = scope === 'period' ? lotteryTimeStamp(values.end) : 0
    if (scope === 'period' && (!start || end <= start)) {
      form.setError('end', { message: t('Choose a valid top-up period') })
      return
    }
    const total = Math.round(values.total * factor)
    const threshold = Math.round(values.threshold * factor)
    const fixed = mode === 'fixed' ? Math.round(values.fixed * factor) : 0
    if (![total, threshold, fixed].every(Number.isSafeInteger) || total < 1) {
      form.setError('total', { message: t('Please enter a valid amount') })
      return
    }
    if (
      mode.endsWith('_percent') &&
      (!Number.isFinite(values.minimum) ||
        !Number.isFinite(values.maximum) ||
        values.minimum < 0.01 ||
        values.maximum > 100 ||
        values.minimum > values.maximum)
    ) {
      form.setError('minimum', {
        message: t('Use a percentage range between 0.01 and 100'),
      })
      return
    }
    if (
      mode === 'random_shares' &&
      (!Number.isInteger(values.shares) ||
        values.shares < 1 ||
        values.shares > Math.min(total, 2147483647))
    ) {
      form.setError('shares', {
        message: t('Please enter a valid share count'),
      })
      return
    }
    if (mode === 'fixed' && (fixed < 1 || fixed > total)) {
      form.setError('fixed', {
        message: t(
          'The fixed reward must be positive and not exceed the prize pool'
        ),
      })
      return
    }
    save.mutate({
      name: values.name,
      mode,
      total_quota: total,
      min_topup_quota: threshold,
      topup_scope: scope,
      topup_start: start,
      topup_end: end,
      per_user_limit: values.limit,
      min_percent: mode.endsWith('_percent')
        ? Math.round(values.minimum * 100)
        : 0,
      max_percent: mode.endsWith('_percent')
        ? Math.round(values.maximum * 100)
        : 0,
      shares: mode === 'random_shares' ? values.shares : 0,
      fixed_quota: fixed,
    })
  })
  const numbers: {
    name:
      | 'total'
      | 'threshold'
      | 'limit'
      | 'minimum'
      | 'maximum'
      | 'shares'
      | 'fixed'
    label: string
    step: number
    min: number
    max?: number
  }[] = [
    {
      name: 'total',
      label: `${t('Total prize pool')} (${unit})`,
      step: 0.01,
      min: 0.01,
    },
    {
      name: 'threshold',
      label: `${t(scope === 'period' ? 'Period top-up required' : 'Cumulative top-up required')} (${unit})`,
      step: 0.01,
      min: 0,
    },
    { name: 'limit', label: t('Draws per user per activity'), step: 1, min: 1 },
  ]
  if (mode.endsWith('_percent')) {
    numbers.push(
      {
        name: 'minimum',
        label: `${t('Minimum reward percentage')} (%)`,
        step: 0.01,
        min: 0.01,
        max: 100,
      },
      {
        name: 'maximum',
        label: `${t('Maximum reward percentage')} (%)`,
        step: 0.01,
        min: 0.01,
        max: 100,
      }
    )
  }
  if (mode === 'random_shares') {
    numbers.push({
      name: 'shares',
      label: t('Total prize shares'),
      step: 1,
      min: 1,
    })
  }
  if (mode === 'fixed') {
    numbers.push({
      name: 'fixed',
      label: `${t('Reward per draw')} (${unit})`,
      step: 0.01,
      min: 0.01,
    })
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) props.onClose()
      }}
      title={t(a ? 'Edit draw activity' : 'Create draw activity')}
      footer={
        <>
          <Button
            variant='outline'
            disabled={save.isPending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button disabled={save.isPending} onClick={() => void submit()}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form onSubmit={submit} className='grid gap-4 sm:grid-cols-2'>
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem className='sm:col-span-2'>
                <FormLabel>{t('Activity name')}</FormLabel>
                <FormControl>
                  <Input {...field} maxLength={80} disabled={save.isPending} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='mode'
            render={({ field }) => (
              <FormItem className='sm:col-span-2'>
                <FormLabel>{t('Reward mode')}</FormLabel>
                <Select
                  value={field.value}
                  onValueChange={field.onChange}
                  disabled={save.isPending}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue>{t(LOTTERY_MODES[field.value])}</SelectValue>
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectGroup>
                      {Object.entries(LOTTERY_MODES).map(([key, label]) => (
                        <SelectItem key={key} value={key}>
                          {t(label)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='scope'
            render={({ field }) => (
              <FormItem className='sm:col-span-2'>
                <FormLabel>{t('Top-up eligibility')}</FormLabel>
                <Select
                  value={field.value}
                  onValueChange={field.onChange}
                  disabled={save.isPending}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue>
                        {t(
                          field.value === 'period'
                            ? 'Top-ups within a period'
                            : 'Lifetime top-ups'
                        )}
                      </SelectValue>
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='lifetime'>
                        {t('Lifetime top-ups')}
                      </SelectItem>
                      <SelectItem value='period'>
                        {t('Top-ups within a period')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          {scope === 'period'
            ? (['start', 'end'] as const).map((name) => (
                <FormField
                  key={name}
                  control={form.control}
                  name={name}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t(
                          name === 'start'
                            ? 'Top-up start (Beijing)'
                            : 'Top-up end (Beijing)'
                        )}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='datetime-local'
                          step={1}
                          {...field}
                          disabled={save.isPending}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ))
            : null}
          {numbers.map((input) => (
            <FormField
              key={input.name}
              control={form.control}
              name={input.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{input.label}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      {...field}
                      value={Number.isNaN(field.value) ? '' : field.value}
                      min={input.min}
                      max={input.max}
                      step={input.step}
                      disabled={save.isPending}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </form>
      </Form>
    </Dialog>
  )
}
