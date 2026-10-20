// 抽奖资格按北京时间配置，不能随管理员浏览器所在时区偏移。
export function lotteryTimeValue(timestamp: number) {
  if (!timestamp) return ''
  return new Date((timestamp + 8 * 3600) * 1000).toISOString().slice(0, 19)
}

export function lotteryTimeStamp(value: string) {
  // 部分日期控件保留零毫秒；整秒仍按原值解析，不接受非零毫秒舍入。
  const seconds = value.replace(/\.0{1,3}$/, '')
  const normalized = seconds.length === 16 ? `${seconds}:00` : seconds
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/.test(normalized)) return 0
  const timestamp = Date.parse(`${normalized}+08:00`) / 1000
  if (!Number.isSafeInteger(timestamp) || timestamp <= 0) return 0
  return lotteryTimeValue(timestamp) === normalized ? timestamp : 0
}

export function lotteryPeriod(start: number, end: number) {
  return `${lotteryTimeValue(start).replace('T', ' ')} — ${lotteryTimeValue(end).replace('T', ' ')}`
}
