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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { adjustUserQuota } from '../api'
import type { QuotaAdjustMode } from '../types'

interface UserQuotaDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  currentQuota: number
  currentGiftQuota?: number
  onSuccess: () => void
}

export function UserQuotaDialog(props: UserQuotaDialogProps) {
  const { t } = useTranslation()
  const [quotaType, setQuotaType] = useState<'balance' | 'gift'>('balance')
  const [mode, setMode] = useState<QuotaAdjustMode>('add')
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(false)

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  // 总余额始终包含赠金，普通余额调整只影响非赠金部分。
  const selectedQuota =
    quotaType === 'gift'
      ? (props.currentGiftQuota ?? 0)
      : props.currentQuota - (props.currentGiftQuota ?? 0)
  const amountValue = Number.parseFloat(amount) || 0
  const quotaValue = parseQuotaFromDollars(Math.abs(amountValue))

  const getPreviewText = () => {
    const current = selectedQuota
    const val = quotaValue
    switch (mode) {
      case 'add':
        return `${t('Current quota')}: ${formatQuota(current)}  +${formatQuota(val)} = ${formatQuota(current + val)}`
      case 'subtract':
        return `${t('Current quota')}: ${formatQuota(current)}  -${formatQuota(val)} = ${formatQuota(current - val)}`
      case 'override': {
        const overrideQuota = parseQuotaFromDollars(amountValue)
        return `${t('Current quota')}: ${formatQuota(current)} → ${formatQuota(overrideQuota)}`
      }
      default:
        return ''
    }
  }

  const handleConfirm = async () => {
    if (loading || !amount.trim() || !Number.isFinite(Number(amount))) return
    if (quotaValue <= 0 && mode !== 'override') return

    if (
      quotaType === 'gift' &&
      ((mode === 'subtract' && quotaValue > selectedQuota) ||
        (mode === 'override' && amountValue < 0))
    ) {
      toast.error(t('Insufficient gift balance'))
      return
    }
    setLoading(true)
    try {
      const value =
        mode === 'override' ? parseQuotaFromDollars(amountValue) : quotaValue
      const result = await adjustUserQuota({
        id: props.userId,
        action: 'add_quota',
        quota_type: quotaType,
        mode,
        value: mode === 'override' ? value : Math.abs(value),
      })
      if (result.success) {
        toast.success(t('Quota adjusted successfully'))
        setAmount('')
        setMode('add')
        setQuotaType('balance')
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        handleServerError(result, t('Failed to adjust quota'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Failed to adjust quota'))
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    setAmount('')
    setMode('add')
    setQuotaType('balance')
    props.onOpenChange(false)
  }

  const placeholder = tokensOnly
    ? t('Enter amount in tokens')
    : t('Enter amount in {{currency}}', { currency: currencyLabel })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (loading) return
        if (!open) handleCancel()
        else props.onOpenChange(true)
      }}
      title={t('Adjust Quota')}
      description={t('Select an operation mode and enter the amount')}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={handleCancel} disabled={loading}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={loading || !amount.trim()}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='grid grid-cols-2 gap-2 text-sm tabular-nums'>
          <div>
            {t('Regular Balance')}:{' '}
            {formatQuota(props.currentQuota - (props.currentGiftQuota ?? 0))}
          </div>
          <div>
            {t('Gift Balance')}: {formatQuota(props.currentGiftQuota ?? 0)}
          </div>
        </div>
        <div className='space-y-2'>
          <Label>{t('Balance Type')}</Label>
          <div
            className='flex gap-1'
            role='group'
            aria-label={t('Balance Type')}
          >
            {(['balance', 'gift'] as const).map((target) => (
              <Button
                key={target}
                type='button'
                variant={quotaType === target ? 'default' : 'outline'}
                size='sm'
                aria-pressed={quotaType === target}
                disabled={loading}
                onClick={() => {
                  setQuotaType(target)
                  setAmount('')
                }}
              >
                {target === 'gift' ? t('Gift Balance') : t('Regular Balance')}
              </Button>
            ))}
          </div>
        </div>
        <div className='text-muted-foreground text-sm'>{getPreviewText()}</div>

        <div className='space-y-2'>
          <Label>{t('Mode')}</Label>
          <div className='flex gap-1'>
            {(['add', 'subtract', 'override'] as const).map((m) => (
              <Button
                key={m}
                type='button'
                variant={mode === m ? 'default' : 'outline'}
                size='sm'
                aria-pressed={mode === m}
                disabled={loading}
                onClick={() => {
                  setMode(m)
                  setAmount('')
                }}
              >
                {m === 'add' && t('Add')}
                {!(m === 'add') && m === 'subtract' && t('Subtract')}
                {!(m === 'add') && !(m === 'subtract') && t('Override')}
              </Button>
            ))}
          </div>
        </div>

        <div className='space-y-2'>
          <Label htmlFor='wallet-quota-amount'>
            {t('Amount')} ({currencyLabel})
          </Label>
          <Input
            id='wallet-quota-amount'
            disabled={loading}
            type='number'
            step={tokensOnly ? 1 : 0.000001}
            min={mode === 'override' && quotaType !== 'gift' ? undefined : 0}
            placeholder={placeholder}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleConfirm()
            }}
          />
        </div>
      </div>
    </Dialog>
  )
}
