import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'

// 设置输入与钱包展示共用货币换算；接口始终传整数额度，避免把元与内部额度混用。
export function lotteryCurrency() {
  const { config, meta } = getCurrencyDisplay()
  if (meta.kind === 'tokens') return { factor: 1, unit: 'Tokens' }
  return { factor: config.quotaPerUnit / meta.exchangeRate, unit: meta.symbol }
}

export function lotteryMoney(quota: number, locale?: string) {
  return formatQuotaWithCurrency(quota, {
    locale,
    abbreviate: false,
    digitsSmall: 6,
    minimumNonZero: 0.000001,
  })
}
