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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useAnnouncements } from '@/features/dashboard/hooks/use-status-data'
import { formatDateTimeObject } from '@/lib/time'
import { useAuthStore } from '@/stores/auth-store'
import { useHomeAnnouncementPopupStore } from '@/stores/home-announcement-popup-store'

export function HomeAnnouncementPopup() {
  const { t } = useTranslation()
  const { items, loading } = useAnnouncements()
  const user = useAuthStore((state) => state.auth.user)
  const suppressedUntilDate = useHomeAnnouncementPopupStore(
    (state) => state.suppressedUntilDate
  )
  const suppressUntilToday = useHomeAnnouncementPopupStore(
    (state) => state.suppressUntilToday
  )
  const [open, setOpen] = useState(false)
  const [currentAnnouncementIndex, setCurrentAnnouncementIndex] = useState(0)
  const [shownQueueSignature, setShownQueueSignature] = useState<string | null>(
    null
  )

  const popupAnnouncements = useMemo(() => {
    if (!user || loading || suppressedUntilDate === new Date().toDateString()) {
      return null
    }

    return items
      .filter((item) => item.popup)
      .sort(
        (left, right) =>
          (left.popupOrder ?? Number.MAX_SAFE_INTEGER) -
            (right.popupOrder ?? Number.MAX_SAFE_INTEGER) ||
          new Date(right.publishDate || 0).getTime() -
            new Date(left.publishDate || 0).getTime() ||
          String(left.id ?? '').localeCompare(String(right.id ?? ''))
      )
  }, [items, loading, suppressedUntilDate, user])

  const popupQueueSignature = useMemo(
    () =>
      popupAnnouncements?.length
        ? JSON.stringify(
            popupAnnouncements.map((item) => ({
              id: item.id,
              popupOrder: item.popupOrder,
              publishDate: item.publishDate,
              title: item.title,
              content: item.content,
              extra: item.extra,
            }))
          )
        : '',
    [popupAnnouncements]
  )
  const announcement = popupAnnouncements?.[currentAnnouncementIndex] ?? null

  useEffect(() => {
    if (!popupQueueSignature) {
      setOpen(false)
      setCurrentAnnouncementIndex(0)
      setShownQueueSignature(null)
      return
    }
    if (shownQueueSignature === popupQueueSignature) {
      return
    }

    setCurrentAnnouncementIndex(0)
    setOpen(true)
    setShownQueueSignature(popupQueueSignature)
  }, [popupQueueSignature, shownQueueSignature])

  const showNextAnnouncement = () => {
    if (
      popupAnnouncements &&
      currentAnnouncementIndex + 1 < popupAnnouncements.length
    ) {
      setCurrentAnnouncementIndex((index) => index + 1)
      return
    }
    setOpen(false)
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setOpen(true)
      return
    }
    showNextAnnouncement()
  }

  const handleSuppressUntilToday = () => {
    suppressUntilToday()
    setOpen(false)
  }

  if (!user || !announcement) {
    return null
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      title={announcement?.title?.trim() || t('Announcement')}
      description={
        announcement?.publishDate
          ? `${t('Published:')} ${formatDateTimeObject(new Date(announcement.publishDate))}`
          : undefined
      }
      contentClassName='sm:max-w-lg'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={showNextAnnouncement}>
            {popupAnnouncements &&
            currentAnnouncementIndex + 1 < popupAnnouncements.length
              ? t('Next announcement')
              : t('Close')}
          </Button>
          <Button variant='outline' onClick={handleSuppressUntilToday}>
            {t('Do not show again today')}
          </Button>
        </>
      }
    >
      <ScrollArea className='max-h-[min(58vh,520px)] pr-4'>
        <div className='space-y-4'>
          {announcement?.content && (
            <RichContent breaks content={announcement.content} />
          )}
          {announcement?.extra && (
            <div className='text-muted-foreground'>
              <RichContent breaks content={announcement.extra} />
            </div>
          )}
        </div>
      </ScrollArea>
    </Dialog>
  )
}
