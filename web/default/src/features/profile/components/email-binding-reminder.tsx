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
import { useLocation } from '@tanstack/react-router'
import { useState } from 'react'

import { useAuthStore } from '@/stores/auth-store'

import { EmailBindDialog } from './dialogs/email-bind-dialog'

export function EmailBindingReminder() {
  const locationHref = useLocation({ select: (location) => location.href })
  const user = useAuthStore((state) => state.auth.user)
  const setUser = useAuthStore((state) => state.auth.setUser)
  const [dismissedLocation, setDismissedLocation] = useState<string | null>(
    null
  )
  const needsEmailBinding = !user?.email?.trim()
  const open = needsEmailBinding && dismissedLocation !== locationHref

  const handleOpenChange = (nextOpen: boolean) => {
    setDismissedLocation(nextOpen ? null : locationHref)
  }

  const handleSuccess = (email: string) => {
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
