import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { NotificationTabs } from '@/components/notification-popover'
import { Button } from '@/components/ui/button'
import { useNotifications } from '@/hooks/use-notifications'
import { useNotificationStore } from '@/stores/notification-store'

export function NoticePopup() {
  const { t } = useTranslation()
  // 挂在根页面：站内切换不重复提醒，刷新或重新打开网页后恢复。
  const [dismissed, setDismissed] = useState(false)
  const [activeTab, setActiveTab] = useState<'notice' | 'announcements' | null>(
    null
  )
  const closedUntilDate = useNotificationStore((state) => state.closedUntilDate)
  const setClosedUntilDate = useNotificationStore(
    (state) => state.setClosedUntilDate
  )
  const notifications = useNotifications()
  const hasContent = Boolean(
    notifications.notice || notifications.announcements.length
  )
  const hiddenToday = closedUntilDate === new Date().toDateString()

  return (
    <Dialog
      open={hasContent && !notifications.loading && !dismissed && !hiddenToday}
      onOpenChange={(open) => {
        if (!open) setDismissed(true)
      }}
      title={t('System Notice')}
      description={t('Latest platform updates and notices')}
      bodyClassName='break-words'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => {
              setDismissed(true)
              try {
                // 使用浏览器本地日期，跨天重新打开时自动恢复提醒。
                setClosedUntilDate(new Date().toDateString())
              } catch {
                // 存储被禁用时仍允许关闭，本次页面不再重复弹出。
              }
            }}
          >
            {t("Don't show again today")}
          </Button>
          <Button onClick={() => setDismissed(true)}>{t('Close')}</Button>
        </>
      }
    >
      <NotificationTabs
        activeTab={
          activeTab ??
          (notifications.announcements.length ? 'announcements' : 'notice')
        }
        onTabChange={setActiveTab}
        notice={notifications.notice}
        announcements={notifications.announcements}
        loading={notifications.loading}
      />
    </Dialog>
  )
}
