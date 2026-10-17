import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { NotificationTabs } from '@/components/notification-popover'
import { Button } from '@/components/ui/button'
import { useNotifications } from '@/hooks/use-notifications'
import { useNotificationStore } from '@/stores/notification-store'

export function NoticePopup() {
  const notifications = useNotifications(true)
  // 首次数据就绪后才建立关闭基线，不能把本地旧缓存当成最新公告。
  if (notifications.loading) return null
  return <NoticePopupContent notifications={notifications} />
}

function NoticePopupContent({
  notifications,
}: {
  notifications: ReturnType<typeof useNotifications>
}) {
  const { t } = useTranslation()
  const [dismissedKeys, setDismissedKeys] = useState<string[] | null>(null)
  // 兼容旧版只有日期的免打扰记录，后续发布的新内容仍可突破旧记录。
  const [initialKeys] = useState(notifications.contentKeys)
  const [selectedTab, setSelectedTab] = useState<{
    revision: string
    tab: 'notice' | 'announcements'
  } | null>(null)
  const closedUntilDate = useNotificationStore((state) => state.closedUntilDate)
  const closedContentKeys = useNotificationStore(
    (state) => state.closedContentKeys
  )
  const setClosedUntilDate = useNotificationStore(
    (state) => state.setClosedUntilDate
  )
  const hasContent = Boolean(
    notifications.notice || notifications.announcements.length
  )
  const hiddenKeys = closedContentKeys ?? initialKeys
  const hiddenToday =
    closedUntilDate === new Date().toDateString() &&
    notifications.contentKeys.every((key) => hiddenKeys.includes(key))
  const dismissed =
    dismissedKeys !== null &&
    notifications.contentKeys.every((key) => dismissedKeys.includes(key))
  const revision = JSON.stringify(notifications.contentKeys)
  const previousKeys = dismissedKeys ?? hiddenKeys
  const newTimeline = notifications.contentKeys.some(
    (key) => key.startsWith('announcement:') && !previousKeys.includes(key)
  )
  const newNotice = notifications.contentKeys.some(
    (key) => key.startsWith('notice:') && !previousKeys.includes(key)
  )
  let activeTab: 'notice' | 'announcements' = notifications.announcements.length
    ? 'announcements'
    : 'notice'
  if (newNotice && !newTimeline) activeTab = 'notice'
  if (selectedTab?.revision === revision) activeTab = selectedTab.tab
  const dismiss = () => {
    // 合并已经关闭的版本，删除或调整公告顺序不会重新打扰。
    setDismissedKeys([
      ...new Set([...(dismissedKeys ?? []), ...notifications.contentKeys]),
    ])
    setSelectedTab(null)
  }

  return (
    <Dialog
      open={hasContent && !notifications.loading && !dismissed && !hiddenToday}
      onOpenChange={(open) => {
        if (!open) dismiss()
      }}
      title={t('System Notice')}
      description={t('Latest platform updates and notices')}
      bodyClassName='break-words'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => {
              dismiss()
              try {
                // 使用浏览器本地日期，跨天重新打开时自动恢复提醒。
                setClosedUntilDate(
                  new Date().toDateString(),
                  notifications.contentKeys
                )
              } catch {
                // 存储被禁用时仍允许关闭，本次页面不再重复弹出。
              }
            }}
          >
            {t("Don't show again today")}
          </Button>
          <Button onClick={dismiss}>{t('Close')}</Button>
        </>
      }
    >
      <NotificationTabs
        activeTab={activeTab}
        onTabChange={(tab) => setSelectedTab({ revision, tab })}
        notice={notifications.notice}
        announcements={notifications.announcements}
        loading={notifications.loading}
      />
    </Dialog>
  )
}
