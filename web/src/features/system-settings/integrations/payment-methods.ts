// 旧配置没有收款码条目时补齐编辑入口，不覆盖管理员配置或非法 JSON。
export function withReceiptPaymentMethod(value: string): string {
  try {
    const parsed: unknown = JSON.parse(value || '[]')
    if (!Array.isArray(parsed)) return value
    if (
      parsed.some(
        (item) =>
          item && typeof item === 'object' && item.type === 'alipay_receipt'
      )
    ) {
      return value
    }
    return JSON.stringify(
      [
        ...parsed,
        { name: '支付宝收款码', type: 'alipay_receipt', icon: 'SiAlipay' },
      ],
      null,
      2
    )
  } catch {
    return value
  }
}
