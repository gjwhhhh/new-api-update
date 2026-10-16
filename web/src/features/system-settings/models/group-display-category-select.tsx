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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import type { GroupDisplayCategory } from '@/lib/group-display'

const automaticCategoryValue = '__automatic_group_display_category__,'

type GroupDisplayCategorySelectProps = {
  group: string
  categories: GroupDisplayCategory[]
  currentCategoryID: string
  automatic: boolean
  onMove: (categoryID: string | null) => void
}

export function GroupDisplayCategorySelect(
  props: GroupDisplayCategorySelectProps
) {
  const { t } = useTranslation()
  const options = useMemo(() => {
    const categoryOptions = props.categories
      .filter((category) => category.id !== props.currentCategoryID)
      .map((category) => ({ value: category.id, label: category.name }))
    if (props.automatic) return categoryOptions
    return [
      { value: automaticCategoryValue, label: t('Automatic matching') },
      ...categoryOptions,
    ]
  }, [props.automatic, props.categories, props.currentCategoryID, t])

  if (options.length === 0) return null

  return (
    <Combobox
      options={options}
      value=''
      onValueChange={(value: string | null) => {
        if (!value) return
        props.onMove(value === automaticCategoryValue ? null : value)
      }}
      placeholder={t('Move to category')}
      searchPlaceholder={t('Move to category')}
      emptyText={t('No display categories available')}
      aria-label={t('Move {{group}} to category', { group: props.group })}
      className='w-44'
    />
  )
}
