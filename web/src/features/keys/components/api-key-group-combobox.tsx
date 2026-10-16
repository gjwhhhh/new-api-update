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
import {
  Check,
  ChevronDown,
  ChevronRight,
  ChevronsUpDown,
  LayoutGrid,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { useMediaQuery } from '@/hooks'
import type { GroupDisplayCategory } from '@/lib/group-display'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveModelProvider } from '@/lib/model-provider'
import { cn } from '@/lib/utils'

import {
  AUTO_GROUP_FRAME_CLASS_NAME,
  AutoGroupFlowBorder,
  GroupRatioBadge,
} from './auto-group-visuals'

export type ApiKeyGroupOption = {
  value: string
  label: string
  desc?: string
  ratio?: number | string
  displayGroup?: GroupDisplayCategory
  displayOrder?: number
}

type ApiKeyGroupComboboxProps = {
  options: ApiKeyGroupOption[]
  value?: string
  onValueChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
}

type ResolvedDisplayCategory = GroupDisplayCategory & {
  options: ApiKeyGroupOption[]
}

const domesticProviders = new Set([
  '360 AI',
  'Baidu',
  'Baichuan',
  'DeepSeek',
  'Doubao',
  'iFlytek',
  'InternLM',
  'MiniMax',
  'MiMo',
  'Moonshot',
  'Qwen',
  'StepFun',
  'Tencent',
  'Yi',
  'Zhipu',
])
const providerCategoryOrders = new Map([
  ['OpenAI', 100],
  ['Anthropic', 200],
  ['Gemini', 300],
  ['xAI', 400],
])
// Group names cannot contain commas, so this value cannot collide with a real option.
const inactiveCommandValue = '__inactive_api_key_group_option__,'

function providerCategoryID(name: string): string {
  return `provider:${name.toLowerCase().replaceAll(/[^a-z0-9]+/g, '-')}`
}

function providerCategoryOrder(name: string): number {
  return providerCategoryOrders.get(name) ?? 600
}

export function ApiKeyGroupCombobox(props: ApiKeyGroupComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const [activeOptionValue, setActiveOptionValue] =
    useState(inactiveCommandValue)
  const [activeCategoryID, setActiveCategoryID] = useState('all')
  const [expandedCategoryID, setExpandedCategoryID] = useState<string | null>(
    null
  )
  const shouldReduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const selectedOption = props.options.find(
    (option) => option.value === props.value
  )
  const isAutoSelected = selectedOption?.value === 'auto'

  const categories = useMemo<ResolvedDisplayCategory[]>(() => {
    const categoryMap = new Map<string, ResolvedDisplayCategory>()
    const ensureCategory = (category: GroupDisplayCategory) => {
      const existing = categoryMap.get(category.id)
      if (existing) return existing
      const created = { ...category, options: [] }
      categoryMap.set(category.id, created)
      return created
    }

    props.options.forEach((option) => {
      if (option.value === 'auto') return
      let category = option.displayGroup
      if (!category) {
        const provider = resolveModelProvider(
          `${option.value} ${option.label} ${option.desc || ''}`
        )
        if (provider && domesticProviders.has(provider.name)) {
          category = {
            id: 'provider:domestic',
            name: t('Domestic models'),
            icon: '',
            order: 500,
          }
        } else if (provider) {
          category = {
            id: providerCategoryID(provider.name),
            name:
              provider.name === 'OpenAI'
                ? 'GPT'
                : provider.label || provider.name,
            icon: provider.icon,
            order: providerCategoryOrder(provider.name),
          }
        } else {
          category = {
            id: 'other',
            name: t('Other groups'),
            icon: '',
            order: 10_000,
          }
        }
      }
      ensureCategory(category).options.push(option)
    })

    return [...categoryMap.values()]
      .map((category) => ({
        ...category,
        options: category.options.toSorted(
          (left, right) =>
            (left.displayOrder ?? 10_000) - (right.displayOrder ?? 10_000) ||
            left.label.localeCompare(right.label)
        ),
      }))
      .toSorted(
        (left, right) =>
          left.order - right.order || left.name.localeCompare(right.name)
      )
  }, [props.options, t])

  const categoryByOption = useMemo(() => {
    const result = new Map<string, ResolvedDisplayCategory>()
    categories.forEach((category) => {
      category.options.forEach((option) => result.set(option.value, category))
    })
    return result
  }, [categories])
  const selectedCategory = selectedOption
    ? categoryByOption.get(selectedOption.value)
    : undefined
  const autoOption = props.options.find((option) => option.value === 'auto')
  const search = searchValue.trim().toLowerCase()
  const searchedCategories = useMemo(
    () =>
      categories
        .map((category) => {
          if (!search || category.name.toLowerCase().includes(search)) {
            return category
          }
          return {
            ...category,
            options: category.options.filter((option) => {
              const ratioText = String(option.ratio ?? '').toLowerCase()
              return (
                option.value.toLowerCase().includes(search) ||
                option.label.toLowerCase().includes(search) ||
                option.desc?.toLowerCase().includes(search) ||
                ratioText.includes(search)
              )
            }),
          }
        })
        .filter((category) => category.options.length > 0),
    [categories, search]
  )
  let visibleCategories = categories
  if (search) {
    visibleCategories = searchedCategories
  }
  const showAuto =
    autoOption &&
    (!search ||
      autoOption.label.toLowerCase().includes(search) ||
      autoOption.desc?.toLowerCase().includes(search))

  const handleSelect = (selectedValue: string) => {
    props.onValueChange(selectedValue)
    setOpen(false)
    setSearchValue('')
  }

  const renderOption = (option: ApiKeyGroupOption) => {
    const isAutoOption = option.value === 'auto'
    return (
      <CommandItem
        key={option.value}
        value={option.value}
        data-auto-group-effect={isAutoOption ? 'option' : undefined}
        onSelect={() => handleSelect(option.value)}
        onPointerLeave={() => {
          if (activeOptionValue === option.value) {
            setActiveOptionValue(inactiveCommandValue)
          }
        }}
        className={cn(
          'data-[selected=true]:bg-muted items-start gap-3 rounded-lg px-3 py-3 transition-colors',
          props.value === option.value && 'bg-primary/6',
          isAutoOption &&
            cn(
              AUTO_GROUP_FRAME_CLASS_NAME,
              'border-primary/35 data-[selected=true]:border-primary/55'
            )
        )}
      >
        {isAutoOption ? (
          <AutoGroupFlowBorder shouldReduceMotion={shouldReduceMotion} />
        ) : null}
        <Check
          aria-hidden='true'
          className={cn(
            'mt-0.5 size-4',
            props.value === option.value ? 'opacity-100' : 'opacity-0'
          )}
        />
        <span className='min-w-0 flex-1'>
          <span className='block truncate font-medium'>{option.label}</span>
          {option.desc ? (
            <span className='text-muted-foreground block truncate text-xs'>
              {option.desc}
            </span>
          ) : null}
        </span>
        <GroupRatioBadge
          ratio={option.ratio}
          isAuto={isAutoOption}
          shouldReduceMotion={shouldReduceMotion}
        />
      </CommandItem>
    )
  }

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        setActiveOptionValue(inactiveCommandValue)
        if (!nextOpen) return
        const categoryID = selectedCategory?.id || 'all'
        setActiveCategoryID(categoryID)
        setExpandedCategoryID(selectedCategory?.id || categories[0]?.id || null)
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            data-auto-group-effect={isAutoSelected ? 'trigger' : undefined}
            disabled={props.disabled}
            className={cn(
              'border-input bg-muted/40 hover:bg-muted/55 hover:text-foreground active:bg-background data-popup-open:border-ring data-popup-open:bg-background data-popup-open:ring-ring/20 relative h-auto min-h-14 w-full justify-between gap-2 rounded-lg px-3 py-2 text-start shadow-none transition-[background-color,border-color,box-shadow] duration-150 data-popup-open:ring-[3px] sm:min-h-20 sm:gap-3 sm:px-4 sm:py-3',
              isAutoSelected &&
                cn(
                  AUTO_GROUP_FRAME_CLASS_NAME,
                  'hover:border-primary/55 data-popup-open:border-primary/55 data-popup-open:ring-primary/20'
                )
            )}
          />
        }
      >
        {isAutoSelected ? (
          <AutoGroupFlowBorder shouldReduceMotion={shouldReduceMotion} />
        ) : null}
        <span className='flex min-w-0 flex-1 items-center gap-3'>
          {selectedCategory?.icon ? (
            <span
              aria-hidden='true'
              className='bg-background flex size-8 shrink-0 items-center justify-center rounded-lg'
            >
              {getLobeIcon(selectedCategory.icon, 22)}
            </span>
          ) : null}
          <span className='flex min-w-0 flex-1 items-center justify-between gap-2 sm:gap-3'>
            <span className='min-w-0'>
              <span className='block truncate font-medium'>
                {selectedOption?.label ||
                  props.placeholder ||
                  t('Select a group')}
              </span>
              {selectedOption?.desc ? (
                <span className='text-muted-foreground block truncate text-[11px] sm:text-xs'>
                  {selectedOption.desc}
                </span>
              ) : null}
            </span>
            <span className='hidden sm:block'>
              <GroupRatioBadge
                ratio={selectedOption?.ratio}
                isAuto={isAutoSelected}
                shouldReduceMotion={shouldReduceMotion}
              />
            </span>
          </span>
        </span>
        <ChevronsUpDown
          aria-hidden='true'
          className='size-4 shrink-0 opacity-50'
        />
      </PopoverTrigger>
      <PopoverContent
        className='data-closed:zoom-out-100 data-open:zoom-in-100 data-[side=bottom]:slide-in-from-top-0 data-[side=left]:slide-in-from-right-0 data-[side=right]:slide-in-from-left-0 data-[side=top]:slide-in-from-bottom-0 w-[min(var(--anchor-width),calc(100vw-2rem))] overflow-hidden rounded-xl p-0 shadow-lg data-closed:duration-75 data-open:duration-100 sm:w-[min(44rem,calc(100vw-2rem))]'
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Command
          shouldFilter={false}
          value={activeOptionValue}
          onValueChange={setActiveOptionValue}
        >
          <CommandInput
            placeholder={t('Search...')}
            value={searchValue}
            onValueChange={(value) => {
              setSearchValue(value)
              setActiveOptionValue(inactiveCommandValue)
            }}
          />
          <div
            role='tablist'
            aria-label={t('Provider categories')}
            className='flex gap-2 overflow-x-auto border-b px-3 py-3'
          >
            <Button
              type='button'
              role='tab'
              aria-selected={activeCategoryID === 'all'}
              variant='outline'
              size='sm'
              className={cn(
                'h-10 shrink-0 gap-2 rounded-lg',
                activeCategoryID === 'all' &&
                  'border-primary bg-primary/8 text-primary'
              )}
              onClick={() => {
                setActiveCategoryID('all')
                setExpandedCategoryID(
                  selectedCategory?.id || categories[0]?.id || null
                )
              }}
            >
              <LayoutGrid className='size-4' aria-hidden='true' />
              {t('All groups')}
              <span
                aria-label={t('{{count}} groups', {
                  count: categories.reduce(
                    (count, category) => count + category.options.length,
                    0
                  ),
                })}
                className='bg-muted rounded-full px-1.5 text-xs tabular-nums'
              >
                {categories.reduce(
                  (count, category) => count + category.options.length,
                  0
                )}
              </span>
            </Button>
            {categories.map((category) => (
              <Button
                key={category.id}
                type='button'
                role='tab'
                aria-selected={activeCategoryID === category.id}
                variant='outline'
                size='sm'
                className={cn(
                  'h-10 shrink-0 gap-2 rounded-lg',
                  activeCategoryID === category.id &&
                    'border-primary bg-primary/8 text-primary'
                )}
                onClick={() => {
                  setActiveCategoryID(category.id)
                  setExpandedCategoryID(category.id)
                }}
              >
                {category.icon ? (
                  <span aria-hidden='true'>
                    {getLobeIcon(category.icon, 18)}
                  </span>
                ) : null}
                {category.name}
                <span
                  aria-label={t('{{count}} groups', {
                    count: category.options.length,
                  })}
                  className='bg-muted rounded-full px-1.5 text-xs tabular-nums'
                >
                  {category.options.length}
                </span>
              </Button>
            ))}
          </div>
          <CommandList className='max-h-[420px] space-y-2 p-3'>
            {showAuto && autoOption ? (
              <div className='mb-2'>{renderOption(autoOption)}</div>
            ) : null}
            {visibleCategories.length === 0 && !showAuto ? (
              <div className='text-muted-foreground px-3 py-8 text-center text-sm'>
                {t('No group found.')}
              </div>
            ) : null}
            {visibleCategories.map((category) => {
              const expanded = search
                ? true
                : expandedCategoryID === category.id
              return (
                <section
                  key={category.id}
                  className='overflow-hidden rounded-xl border'
                >
                  <button
                    type='button'
                    aria-expanded={expanded}
                    className='hover:bg-muted/50 flex w-full items-center gap-3 px-4 py-3 text-start transition-colors'
                    onClick={() => {
                      setActiveCategoryID(category.id)
                      setExpandedCategoryID(expanded ? null : category.id)
                    }}
                  >
                    {category.icon ? (
                      <span
                        aria-hidden='true'
                        className='flex size-7 shrink-0 items-center justify-center'
                      >
                        {getLobeIcon(category.icon, 22)}
                      </span>
                    ) : null}
                    <span className='min-w-0 flex-1 font-semibold'>
                      {category.name}
                    </span>
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {t('{{count}} groups', {
                        count: category.options.length,
                      })}
                    </span>
                    {expanded ? (
                      <ChevronDown className='size-4' aria-hidden='true' />
                    ) : (
                      <ChevronRight className='size-4' aria-hidden='true' />
                    )}
                  </button>
                  {expanded ? (
                    <div className='border-t p-2'>
                      {category.options.map(renderOption)}
                    </div>
                  ) : null}
                </section>
              )
            })}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
