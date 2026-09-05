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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { deleteTaskLog } from '../api'
import type { TaskLog } from '../types'

// 删除入口始终以根用户角色判断，普通管理员同样看不到入口。
export function DeleteTaskLogButton(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const role = useAuthStore((state) => state.auth.user?.role)
  const [open, setOpen] = useState(false)
  const queryClient = useQueryClient()
  const deletion = useMutation({
    mutationFn: () => deleteTaskLog(props.log.id),
    onSuccess: () => {
      setOpen(false)
      toast.success(t('Task log deleted'))
      void queryClient.invalidateQueries({ queryKey: ['logs', 'task'] })
    },
  })
  if (role !== ROLE.SUPER_ADMIN) return null
  const finished =
    props.log.status === 'SUCCESS' || props.log.status === 'FAILURE'
  return (
    <>
      <Button
        variant='ghost'
        size='sm'
        disabled={!finished || deletion.isPending}
        onClick={() => setOpen(true)}
        title={
          finished
            ? t('Delete task log')
            : t('Wait until the task finishes before deleting its log')
        }
      >
        {t('Delete')}
      </Button>
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete task log')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This deletes the task log and its stored media. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deletion.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={deletion.isPending}
              onClick={() => deletion.mutate()}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
