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
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { Reorder } from 'motion/react'
import { nanoid } from 'nanoid'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import { EmptyState } from '@/components/empty-state'
import { LobeIconField } from '@/components/lobe-icon-field'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  parseGroupDisplayConfig,
  serializeGroupDisplayConfig,
  type GroupDisplayConfig,
} from '@/lib/group-display'

type GroupDisplayEditorProps = {
  value: string
  groupOptions: string[]
  onChange: (value: string) => void
}

export function GroupDisplayEditor(props: GroupDisplayEditorProps) {
  const { t } = useTranslation()
  const config = useMemo(
    () => parseGroupDisplayConfig(props.value),
    [props.value]
  )
  const options = useMemo(
    () =>
      props.groupOptions
        .filter((group) => group !== 'auto')
        .map((group) => ({ label: group, value: group })),
    [props.groupOptions]
  )

  const emit = (next: GroupDisplayConfig) => {
    props.onChange(serializeGroupDisplayConfig(next))
  }

  const updateCategory = (
    categoryID: string,
    field: 'name' | 'icon',
    value: string
  ) => {
    emit({
      ...config,
      categories: config.categories.map((category) =>
        category.id === categoryID ? { ...category, [field]: value } : category
      ),
    })
  }

  const moveCategory = (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= config.categories.length) return
    const categories = [...config.categories]
    ;[categories[index], categories[target]] = [
      categories[target],
      categories[index],
    ]
    emit({
      ...config,
      categories: categories.map((category, order) => ({
        ...category,
        order: order * 10,
      })),
    })
  }

  const removeCategory = (categoryID: string) => {
    emit({
      categories: config.categories.filter(
        (category) => category.id !== categoryID
      ),
      assignments: Object.fromEntries(
        Object.entries(config.assignments).filter(
          ([, assignment]) => assignment.category_id !== categoryID
        )
      ),
    })
  }

  const updateAssignments = (categoryID: string, selected: string[]) => {
    const selectedSet = new Set(selected)
    const assignments = Object.fromEntries(
      Object.entries(config.assignments).filter(
        ([group, assignment]) =>
          assignment.category_id !== categoryID || selectedSet.has(group)
      )
    )
    selected.forEach((group, order) => {
      assignments[group] = { category_id: categoryID, order: order * 10 }
    })
    emit({ ...config, assignments })
  }

  const moveAssignment = (
    categoryID: string,
    selected: string[],
    index: number,
    direction: 'up' | 'down'
  ) => {
    const target = direction === 'up' ? index - 1 : index + 1
    if (target < 0 || target >= selected.length) return
    const reordered = [...selected]
    const current = reordered[index]
    reordered[index] = reordered[target]
    reordered[target] = current
    updateAssignments(categoryID, reordered)
  }

  const unassignedGroups = props.groupOptions.filter(
    (group) => group !== 'auto' && !config.assignments[group]
  )

  return (
    <Card className='min-w-0 shadow-none'>
      <CardHeader className='gap-3 border-b'>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>{t('Group display categories')}</CardTitle>
            <CardDescription>
              {t(
                'Organize API key groups into horizontal provider categories. Unassigned groups are categorized automatically.'
              )}
            </CardDescription>
          </div>
          <Button
            type='button'
            size='sm'
            onClick={() => {
              emit({
                ...config,
                categories: [
                  ...config.categories,
                  {
                    id: `display_${nanoid(8)}`,
                    name: t('New display category'),
                    icon: '',
                    order: config.categories.length * 10,
                  },
                ],
              })
            }}
          >
            <Plus className='mr-2 size-4' aria-hidden='true' />
            {t('Add display category')}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        {config.categories.length === 0 ? (
          <EmptyState
            className='min-h-40'
            title={t('Using automatic provider categories')}
            description={t(
              'Groups are inferred from their names and descriptions until you add a custom display category.'
            )}
          />
        ) : (
          config.categories.map((category, index) => {
            const selected = Object.entries(config.assignments)
              .filter(
                ([, assignment]) => assignment.category_id === category.id
              )
              .sort(([, left], [, right]) => left.order - right.order)
              .map(([group]) => group)
            return (
              <div
                key={category.id}
                className='bg-muted/15 space-y-4 rounded-xl border p-4'
              >
                <div className='flex items-start gap-3'>
                  <div className='grid min-w-0 flex-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(15rem,1fr)]'>
                    <div className='space-y-1.5'>
                      <Label htmlFor={`${category.id}-name`}>
                        {t('Display category name')}
                      </Label>
                      <Input
                        id={`${category.id}-name`}
                        value={category.name}
                        onChange={(event) =>
                          updateCategory(
                            category.id,
                            'name',
                            event.target.value
                          )
                        }
                      />
                    </div>
                    <div className='space-y-1.5'>
                      <Label htmlFor={`${category.id}-icon`}>
                        {t('Optional provider icon')}
                      </Label>
                      <LobeIconField
                        id={`${category.id}-icon`}
                        value={category.icon || ''}
                        allowEmpty
                        onChange={(value) =>
                          updateCategory(category.id, 'icon', value)
                        }
                      />
                    </div>
                  </div>
                  <div className='flex shrink-0 gap-1'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={index === 0}
                      aria-label={t('Move up')}
                      onClick={() => moveCategory(index, -1)}
                    >
                      <ArrowUp aria-hidden='true' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={index === config.categories.length - 1}
                      aria-label={t('Move down')}
                      onClick={() => moveCategory(index, 1)}
                    >
                      <ArrowDown aria-hidden='true' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      aria-label={t('Delete display category')}
                      onClick={() => removeCategory(category.id)}
                    >
                      <Trash2 aria-hidden='true' />
                    </Button>
                  </div>
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor={`${category.id}-groups`}>
                    {t('Model groups in this category')}
                  </Label>
                  <MultiSelect
                    id={`${category.id}-groups`}
                    options={options}
                    selected={selected}
                    onChange={(groups) =>
                      updateAssignments(category.id, groups)
                    }
                    placeholder={t('Select model groups')}
                    maxVisibleChips={8}
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'A model group can belong to only one display category. Selecting it here moves it from its previous category.'
                    )}
                  </p>
                  {selected.length > 0 ? (
                    <Reorder.Group
                      as='ol'
                      axis='y'
                      values={selected}
                      onReorder={(groups) =>
                        updateAssignments(category.id, groups)
                      }
                      aria-label={t('Model groups in this category')}
                      className='flex flex-col gap-2 pt-2'
                    >
                      {selected.map((group, groupIndex) => (
                        <AutoGroupOrderItem
                          key={group}
                          group={group}
                          index={groupIndex}
                          count={selected.length}
                          onMove={(itemIndex, direction) =>
                            moveAssignment(
                              category.id,
                              selected,
                              itemIndex,
                              direction
                            )
                          }
                          onRemove={(removedGroup) =>
                            updateAssignments(
                              category.id,
                              selected.filter((item) => item !== removedGroup)
                            )
                          }
                        />
                      ))}
                    </Reorder.Group>
                  ) : null}
                </div>
              </div>
            )
          })
        )}
        {config.categories.length > 0 ? (
          <div className='text-muted-foreground rounded-lg border border-dashed p-3 text-xs'>
            {t('{{count}} groups remain automatically categorized.', {
              count: unassignedGroups.length,
            })}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
