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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, type ComponentProps } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

export type RateLimitTargetType = 'group' | 'model'

const createRateLimitDialogSchema = (t: (key: string) => string) =>
  z.object({
    targetType: z.enum(['group', 'model']),
    target: z.string().trim().min(1, t('Target name is required')),
    maxRequests: z
      .number()
      .int(t('Must be an integer'))
      .min(0, 'Must be ≥ 0')
      .max(2147483647, 'Must be ≤ 2,147,483,647'),
    maxSuccess: z
      .number()
      .int(t('Must be an integer'))
      .min(1, 'Must be ≥ 1')
      .max(2147483647, 'Must be ≤ 2,147,483,647'),
  })

type RateLimitDialogFormValues = z.infer<
  ReturnType<typeof createRateLimitDialogSchema>
>

const RATE_LIMIT_FORM_ID = 'rate-limit-form'

export type RateLimitEntryData = {
  targetType: RateLimitTargetType
  target: string
  maxRequests: number
  maxSuccess: number
}

type RateLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: RateLimitEntryData) => string | null
  editData?: RateLimitEntryData | null
}

type RateLimitTargetTypeToggleProps = Omit<
  ComponentProps<'div'>,
  'onChange'
> & {
  value: RateLimitTargetType
  onChange: (value: RateLimitTargetType) => void
  ariaLabel: string
}

export function RateLimitTargetTypeToggle({
  value,
  onChange,
  ariaLabel,
  className,
  ...props
}: RateLimitTargetTypeToggleProps) {
  const { t } = useTranslation()

  return (
    <div
      role='group'
      aria-label={ariaLabel}
      className={cn(
        'bg-muted/60 inline-flex h-9 items-center rounded-lg border p-0.5',
        className
      )}
      {...props}
    >
      {(['group', 'model'] as const).map((targetType) => {
        const isActive = targetType === value
        return (
          <button
            key={targetType}
            type='button'
            aria-pressed={isActive}
            onClick={() => onChange(targetType)}
            className={cn(
              'inline-flex h-full min-w-20 items-center justify-center rounded-md px-4 text-sm font-medium transition-colors',
              isActive
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            )}
          >
            {t(targetType === 'group' ? 'Group' : 'Model')}
          </button>
        )
      })}
    </div>
  )
}

export function RateLimitDialog({
  open,
  onOpenChange,
  onSave,
  editData,
}: RateLimitDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!editData
  const rateLimitDialogSchema = createRateLimitDialogSchema(t)

  const form = useForm<RateLimitDialogFormValues>({
    resolver: zodResolver(rateLimitDialogSchema),
    defaultValues: {
      targetType: 'group',
      target: '',
      maxRequests: 0,
      maxSuccess: 1,
    },
  })

  useEffect(() => {
    if (editData) {
      form.reset(editData)
    } else {
      form.reset({
        targetType: 'group',
        target: '',
        maxRequests: 0,
        maxSuccess: 1,
      })
    }
  }, [editData, form, open])

  const handleSubmit = (values: RateLimitDialogFormValues) => {
    const saveError = onSave(values)
    if (saveError) {
      form.setError('target', { type: 'manual', message: saveError })
      return
    }
    form.reset()
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={isEditMode ? t('Edit rate limit') : t('Add rate limit')}
      description={t('Configure rate limiting rules for a group or model.')}
      contentClassName='sm:max-w-[500px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={RATE_LIMIT_FORM_ID}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={RATE_LIMIT_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='targetType'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Type')}</FormLabel>
                <FormControl>
                  <RateLimitTargetTypeToggle
                    value={field.value}
                    onChange={(targetType) => {
                      field.onChange(targetType)
                      form.clearErrors('target')
                    }}
                    ariaLabel={t('Type')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='target'
            render={({ field }) => {
              const targetType = form.watch('targetType')
              return (
                <FormItem>
                  <FormLabel>{t('Target')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={
                        targetType === 'group'
                          ? t('e.g., default, vip, premium')
                          : t('e.g., gpt-4o, claude-sonnet')
                      }
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Unique identifier for this target.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )
            }}
          />

          <FormField
            control={form.control}
            name='maxRequests'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max Requests (including failures)')}</FormLabel>
                <FormControl>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='number'
                      min={0}
                      max={2147483647}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value) || 0)
                      }
                    />
                    <span className='text-muted-foreground text-sm'>
                      {t('times')}
                    </span>
                  </div>
                </FormControl>
                <FormDescription>
                  {t('Total requests allowed per period. 0 = unlimited.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='maxSuccess'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max Successful Requests')}</FormLabel>
                <FormControl>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='number'
                      min={1}
                      max={2147483647}
                      step={1}
                      {...field}
                      onChange={(e) =>
                        field.onChange(Number.parseInt(e.target.value) || 1)
                      }
                    />
                    <span className='text-muted-foreground text-sm'>
                      {t('times')}
                    </span>
                  </div>
                </FormControl>
                <FormDescription>
                  {t('Only successful requests count toward this limit.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
