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
import { useEffect, useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import { EmailBindDialog } from './dialogs/email-bind-dialog'

const EMAIL_BINDING_REMINDER_INTERVAL_MS = 5 * 60 * 1000

export function EmailBindingReminder() {
  const user = useAuthStore((state) => state.auth.user)
  const setUser = useAuthStore((state) => state.auth.setUser)
  const storageKey = `email-binding-reminder:${user?.id ?? 'anonymous'}`
  const [nextReminderAt, setNextReminderAt] = useState(() => {
    if (typeof window === 'undefined') return 0
    return Number(window.localStorage.getItem(storageKey)) || 0
  })
  const needsEmailBinding = !user?.email?.trim()
  const open = needsEmailBinding && Date.now() >= nextReminderAt

  useEffect(() => {
    if (typeof window === 'undefined') return
    setNextReminderAt(Number(window.localStorage.getItem(storageKey)) || 0)
  }, [storageKey])

  useEffect(() => {
    if (!needsEmailBinding || nextReminderAt <= Date.now()) return
    const timeout = window.setTimeout(
      () => setNextReminderAt(0),
      nextReminderAt - Date.now()
    )
    return () => window.clearTimeout(timeout)
  }, [needsEmailBinding, nextReminderAt])

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) return
    const nextReminder = Date.now() + EMAIL_BINDING_REMINDER_INTERVAL_MS
    window.localStorage.setItem(storageKey, String(nextReminder))
    setNextReminderAt(nextReminder)
  }

  const handleSuccess = (email: string) => {
    window.localStorage.removeItem(storageKey)
    if (user) setUser({ ...user, email })
  }

  if (!needsEmailBinding) return null

  return (
    <EmailBindDialog
      open={open}
      onOpenChange={handleOpenChange}
      onSuccess={handleSuccess}
    />
  )
}
