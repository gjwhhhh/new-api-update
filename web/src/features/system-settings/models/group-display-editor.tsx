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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { LobeIconField } from '@/components/lobe-icon-field'
import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
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
  isAutomaticGroupDisplayCategoryID,
  resolveAutomaticGroupDisplayCategory,
  serializeGroupDisplayConfig,
  type GroupDisplayCategory,
  type GroupDisplayConfig,
  type GroupDisplayOptionSource,
} from '@/lib/group-display'
import { getLobeIcon } from '@/lib/lobe-icon'

import { GroupDisplayCategorySelect } from './group-display-category-select'

type GroupDisplayEditorProps = {
  value: string
  groupOptions: GroupDisplayOptionSource[]
  onChange: (value: string) => void
}

type AutomaticDisplayCategory = {
  category: GroupDisplayCategory
  groups: string[]
}

type AutomaticDisplayResolution = {
  categories: AutomaticDisplayCategory[]
  inheritedGroups: Record<string, string[]>
}

export function GroupDisplayEditor(props: GroupDisplayEditorProps) {
  const { t } = useTranslation()
  const [resetOpen, setResetOpen] = useState(false)
  const config = useMemo(
    () => parseGroupDisplayConfig(props.value),
    [props.value]
  )
  const options = useMemo(
    () =>
      props.groupOptions
        .filter((group) => group.value !== 'auto')
        .map((group) => ({ label: group.label, value: group.value })),
    [props.groupOptions]
  )
  const automaticResolution = useMemo<AutomaticDisplayResolution>(() => {
    const configuredCategoryIDs = new Set(
      config.categories.map((category) => category.id)
    )
    const categories = new Map<string, AutomaticDisplayCategory>()
    const inheritedGroups: Record<string, string[]> = {}
    for (const option of props.groupOptions) {
      if (option.value === 'auto' || config.assignments[option.value]) {
        continue
      }
      const category = resolveAutomaticGroupDisplayCategory(option, {
        domesticModels: t('Domestic models'),
        otherGroups: t('Other groups'),
      })
      if (configuredCategoryIDs.has(category.id)) {
        inheritedGroups[category.id] = [
          ...(inheritedGroups[category.id] || []),
          option.value,
        ]
        continue
      }
      const existing = categories.get(category.id)
      if (existing) {
        existing.groups.push(option.value)
      } else {
        categories.set(category.id, {
          category,
          groups: [option.value],
        })
      }
    }
    const automaticCategories = [...categories.values()]
      .map((item) => ({
        ...item,
        groups: item.groups.toSorted((left, right) =>
          left.localeCompare(right)
        ),
      }))
      .toSorted(
        (left, right) =>
          left.category.order - right.category.order ||
          left.category.name.localeCompare(right.category.name)
      )
    for (const categoryID of Object.keys(inheritedGroups)) {
      inheritedGroups[categoryID] = inheritedGroups[categoryID].toSorted(
        (left, right) => left.localeCompare(right)
      )
    }
    return { categories: automaticCategories, inheritedGroups }
  }, [config.assignments, config.categories, props.groupOptions, t])
  const automaticCategories = automaticResolution.categories

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

  const moveGroupToCategory = (
    group: string,
    targetCategoryID: string | null
  ) => {
    const assignments = { ...config.assignments }
    if (!targetCategoryID) {
      delete assignments[group]
      emit({ ...config, assignments })
      return
    }
    const nextOrder =
      Math.max(
        -10,
        ...Object.values(assignments)
          .filter((assignment) => assignment.category_id === targetCategoryID)
          .map((assignment) => assignment.order)
      ) + 10
    assignments[group] = {
      category_id: targetCategoryID,
      order: nextOrder,
    }
    emit({ ...config, assignments })
  }

  const customizeAutomaticCategory = (
    automaticCategory: AutomaticDisplayCategory
  ) => {
    emit({
      categories: [...config.categories, automaticCategory.category],
      assignments: config.assignments,
    })
  }

  const unassignedGroups = props.groupOptions.filter(
    (group) => group.value !== 'auto' && !config.assignments[group.value]
  )
  const hasCustomDisplayConfig =
    config.categories.length > 0 || Object.keys(config.assignments).length > 0

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
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={!hasCustomDisplayConfig}
              onClick={() => setResetOpen(true)}
            >
              {t('Reset to automatic categories')}
            </Button>
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
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        {config.categories.length === 0 && automaticCategories.length === 0 ? (
          <EmptyState
            className='min-h-40'
            title={t('Using automatic provider categories')}
            description={t(
              'Groups are inferred from their names and descriptions until you add a custom display category.'
            )}
          />
        ) : null}
        {config.categories.map((category, index) => {
          const assignedGroups = Object.entries(config.assignments)
            .filter(([, assignment]) => assignment.category_id === category.id)
            .sort(([, left], [, right]) => left.order - right.order)
            .map(([group]) => group)
          const inheritedGroups =
            automaticResolution.inheritedGroups[category.id] || []
          const inheritedGroupSet = new Set(inheritedGroups)
          const effectiveGroups = [...assignedGroups, ...inheritedGroups]
          const providerOverride = isAutomaticGroupDisplayCategoryID(
            category.id
          )
          return (
            <section
              key={category.id}
              aria-label={category.name}
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
                        updateCategory(category.id, 'name', event.target.value)
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
                  {providerOverride ? (
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={() => removeCategory(category.id)}
                    >
                      {t('Restore automatic category')}
                    </Button>
                  ) : (
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      aria-label={t('Delete display category')}
                      onClick={() => removeCategory(category.id)}
                    >
                      <Trash2 aria-hidden='true' />
                    </Button>
                  )}
                </div>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor={`${category.id}-groups`}>
                  {providerOverride
                    ? t('Manually assigned model groups')
                    : t('Model groups in this category')}
                </Label>
                <MultiSelect
                  id={`${category.id}-groups`}
                  options={options}
                  selected={assignedGroups}
                  onChange={(groups) => updateAssignments(category.id, groups)}
                  placeholder={t('Select model groups')}
                  maxVisibleChips={8}
                />
                <p className='text-muted-foreground text-xs'>
                  {providerOverride
                    ? t(
                        'Automatically matched groups are listed below. Select another group here to move it into this category.'
                      )
                    : t(
                        'A model group can belong to only one display category. Selecting it here moves it from its previous category.'
                      )}
                </p>
                {effectiveGroups.length > 0 ? (
                  <Reorder.Group
                    as='ol'
                    axis='y'
                    values={effectiveGroups}
                    onReorder={(groups) =>
                      updateAssignments(category.id, groups)
                    }
                    aria-label={t('Model groups in this category')}
                    className='flex flex-col gap-2 pt-2'
                  >
                    {effectiveGroups.map((group, groupIndex) => (
                      <AutoGroupOrderItem
                        key={group}
                        group={group}
                        index={groupIndex}
                        count={effectiveGroups.length}
                        removable={!inheritedGroupSet.has(group)}
                        onMove={(itemIndex, direction) =>
                          moveAssignment(
                            category.id,
                            effectiveGroups,
                            itemIndex,
                            direction
                          )
                        }
                        onRemove={(removedGroup) =>
                          updateAssignments(
                            category.id,
                            assignedGroups.filter(
                              (item) => item !== removedGroup
                            )
                          )
                        }
                      >
                        {inheritedGroupSet.has(group) ? (
                          <Badge variant='secondary'>{t('Auto')}</Badge>
                        ) : null}
                        <GroupDisplayCategorySelect
                          group={group}
                          categories={config.categories}
                          currentCategoryID={category.id}
                          automatic={inheritedGroupSet.has(group)}
                          onMove={(categoryID) =>
                            moveGroupToCategory(group, categoryID)
                          }
                        />
                      </AutoGroupOrderItem>
                    ))}
                  </Reorder.Group>
                ) : null}
              </div>
            </section>
          )
        })}
        {automaticCategories.map((item) => (
          <section
            key={item.category.id}
            aria-label={item.category.name}
            className='bg-muted/10 flex flex-col gap-4 rounded-xl border border-dashed p-4'
          >
            <div className='flex items-center gap-3'>
              {item.category.icon ? (
                <span
                  aria-hidden='true'
                  className='bg-background flex size-10 shrink-0 items-center justify-center rounded-lg border'
                >
                  {getLobeIcon(item.category.icon, 26)}
                </span>
              ) : null}
              <div className='min-w-0 flex-1'>
                <div className='flex flex-wrap items-center gap-2'>
                  <h3 className='truncate font-semibold'>
                    {item.category.name}
                  </h3>
                  <Badge variant='secondary'>{t('Auto')}</Badge>
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t('{{count}} groups', { count: item.groups.length })}
                </p>
              </div>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => customizeAutomaticCategory(item)}
              >
                {t('Edit')}
              </Button>
            </div>
            <div className='flex flex-wrap gap-2'>
              {item.groups.map((group) => (
                <Badge key={group} variant='outline'>
                  {group}
                </Badge>
              ))}
            </div>
          </section>
        ))}
        {config.categories.length > 0 || automaticCategories.length > 0 ? (
          <div className='text-muted-foreground rounded-lg border border-dashed p-3 text-xs'>
            {t('{{count}} groups remain automatically categorized.', {
              count: unassignedGroups.length,
            })}
          </div>
        ) : null}
      </CardContent>
      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={t('Reset display categories?')}
        desc={t(
          'This clears custom display categories, icons, assignments, and ordering, then restores automatic provider matching. Model groups are not deleted.'
        )}
        confirmText={t('Reset')}
        destructive
        handleConfirm={() => {
          emit({ categories: [], assignments: {} })
          setResetOpen(false)
        }}
      />
    </Card>
  )
}
