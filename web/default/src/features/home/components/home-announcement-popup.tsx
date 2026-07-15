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
import type { AnnouncementItem } from '@/features/dashboard/types'
import { formatDateTimeObject } from '@/lib/time'
import { useHomeAnnouncementPopupStore } from '@/stores/home-announcement-popup-store'

export function HomeAnnouncementPopup() {
  const { t } = useTranslation()
  const { items, loading } = useAnnouncements()
  const suppressedUntilDate = useHomeAnnouncementPopupStore(
    (state) => state.suppressedUntilDate
  )
  const suppressUntilToday = useHomeAnnouncementPopupStore(
    (state) => state.suppressUntilToday
  )
  const [announcement, setAnnouncement] = useState<AnnouncementItem | null>(
    null
  )
  const [open, setOpen] = useState(false)
  const [hasAutoOpened, setHasAutoOpened] = useState(false)

  const popupAnnouncement = useMemo(() => {
    if (loading || suppressedUntilDate === new Date().toDateString()) {
      return null
    }

    return items
      .filter((item) => item.popup)
      .sort(
        (left, right) =>
          new Date(right.publishDate || 0).getTime() -
          new Date(left.publishDate || 0).getTime()
      )[0]
  }, [items, loading, suppressedUntilDate])

  useEffect(() => {
    if (hasAutoOpened || !popupAnnouncement) {
      return
    }

    setAnnouncement(popupAnnouncement)
    setOpen(true)
    setHasAutoOpened(true)
  }, [hasAutoOpened, popupAnnouncement])

  const handleSuppressUntilToday = () => {
    suppressUntilToday()
    setOpen(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
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
          <Button variant='outline' onClick={() => setOpen(false)}>
            {t('Close')}
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
