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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { describe, expect, it, vi } from 'vitest'
import { z } from 'zod'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { positiveIntegerSchema } from '../../utils/numeric-field'
import { GroupRatioForm } from '../group-ratio-form'

const defaults = {
  GroupRatio: '{"default":1,"vip":0.8}',
  TopupGroupRatio: '{"vip":1.2}',
  UserUsableGroups: '{"default":"Standard access","vip":"Premium access"}',
  GroupGroupRatio: '{}',
  AutoGroups: '["default","vip"]',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
  GroupDisplayConfig: '{"categories":[],"assignments":{}}',
}

const schema = z.object({
  GroupRatio: z.string(),
  TopupGroupRatio: z.string(),
  UserUsableGroups: z.string(),
  GroupGroupRatio: z.string(),
  AutoGroups: z.string(),
  MaxTokenAutoGroups: positiveIntegerSchema('Enter a positive integer'),
  DefaultUseAutoGroup: z.boolean(),
  GroupSpecialUsableGroup: z.string(),
  GroupDisplayConfig: z.string(),
})

function Fixture(props: {
  onSave?: (values: typeof defaults) => Promise<void>
  initial?: Partial<typeof defaults>
  isSaving?: boolean
}) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const form = useForm({
    defaultValues: { ...defaults, ...props.initial },
    resolver: zodResolver(schema),
  })
  return (
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <GroupRatioForm
        form={form}
        onSave={props.onSave ?? (async () => {})}
        onRename={() => {}}
        isSaving={props.isSaving ?? false}
      />
    </SettingsPageProvider>
  )
}

describe('group settings workspace', () => {
  it('filters by description and clears search without discarding edits', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    const search = screen.getByRole('textbox', {
      name: 'Search groups by name or description',
    })
    await user.type(search, 'PREMIUM')
    expect(screen.getAllByRole('textbox', { name: 'Group name' })).toHaveLength(
      1
    )
    expect(screen.getByRole('textbox', { name: 'Group name' })).toHaveValue(
      'vip'
    )
    await user.clear(screen.getByRole('spinbutton', { name: 'Ratio' }))
    await user.type(screen.getByRole('spinbutton', { name: 'Ratio' }), '0.6')
    await user.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(screen.getAllByRole('textbox', { name: 'Group name' })).toHaveLength(
      2
    )
    expect(screen.getAllByRole('spinbutton', { name: 'Ratio' })[1]).toHaveValue(
      0.6
    )
    await user.type(search, 'missing')
    expect(screen.getByText('No results found')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Add group' }))
    expect(search).toHaveValue('')
    expect(screen.getByDisplayValue('group_1')).toBeVisible()
  })

  it('keeps unfinished visibility rules while switching sections and supports keyboard tabs', async () => {
    const user = userEvent.setup()
    render(
      <Fixture
        initial={{
          GroupSpecialUsableGroup: '{"vip":{"+:default":"Standard"}}',
        }}
      />
    )
    const pricing = screen.getByRole('tab', { name: 'Pricing groups' })
    pricing.focus()
    await user.keyboard('{ArrowRight}{Enter}')
    expect(
      screen.getByRole('tab', { name: 'Special ratio rules' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(
      screen.queryByRole('table', { name: 'Pricing groups' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('tab', { name: 'Group visibility' }))
    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(
      screen.getAllByRole('combobox', { name: 'Group name' })
    ).toHaveLength(2)
    await user.click(pricing)
    await user.click(screen.getByRole('tab', { name: 'Group visibility' }))
    expect(
      screen.getAllByRole('combobox', { name: 'Group name' })
    ).toHaveLength(2)
    const toggle = screen.getByRole('button', { name: 'Rules for vip' })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('reorders and removes auto groups with named controls and saves across sections', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const list = screen.getByRole('list', { name: 'Auto group order' })
    expect(
      screen.getByRole('button', { name: 'Move default up' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move vip down' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Move vip up' }))
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent('vip')
    await user.click(
      screen.getByRole('switch', { name: 'Default to auto groups' })
    )
    await user.click(screen.getByRole('button', { name: 'Remove default' }))
    expect(screen.getByRole('button', { name: 'Move vip up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move vip down' })).toBeDisabled()
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        AutoGroups: '[\n  "vip"\n]',
        DefaultUseAutoGroup: true,
      })
    )
  })

  it('edits custom display categories without changing pricing groups', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          GroupDisplayConfig: JSON.stringify({
            categories: [{ id: 'openai', name: 'GPT', icon: '', order: 0 }],
            assignments: {
              default: { category_id: 'openai', order: 0 },
            },
          }),
        }}
      />
    )

    await user.click(screen.getByRole('tab', { name: 'Group display' }))
    const name = screen.getByRole('textbox', {
      name: 'Display category name',
    })
    expect(name).toHaveValue('GPT')
    expect(screen.getAllByText('default').length).toBeGreaterThan(0)
    await user.clear(name)
    await user.type(name, 'OpenAI models')
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].GroupDisplayConfig)).toEqual({
      categories: [{ id: 'openai', name: 'OpenAI models', icon: '', order: 0 }],
      assignments: {
        default: { category_id: 'openai', order: 0 },
      },
    })
  })

  it('shows automatic provider categories and converts one into an editable category', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          GroupRatio: '{"gpt-pro":1,"claude-team":1}',
          UserUsableGroups:
            '{"gpt-pro":"Stable GPT pool","claude-team":"Claude pool"}',
          TopupGroupRatio: '{}',
          AutoGroups: '[]',
        }}
      />
    )

    await user.click(screen.getByRole('tab', { name: 'Group display' }))
    const gptCategory = screen.getByRole('region', { name: 'GPT' })
    const claudeCategory = screen.getByRole('region', { name: 'Claude' })
    expect(within(gptCategory).getByText('gpt-pro')).toBeVisible()
    expect(within(claudeCategory).getByText('claude-team')).toBeVisible()

    await user.click(within(gptCategory).getByRole('button', { name: 'Edit' }))
    expect(
      screen.getByRole('textbox', { name: 'Display category name' })
    ).toHaveValue('GPT')

    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].GroupDisplayConfig)).toEqual({
      categories: [
        {
          id: 'provider:openai',
          name: 'GPT',
          icon: 'OpenAI.Color',
          order: 100,
        },
      ],
      assignments: {},
    })
  })

  it('resets custom display assignments and reruns automatic provider matching', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          GroupRatio: '{"gpt":1}',
          UserUsableGroups: '{"gpt":"GPT pool"}',
          TopupGroupRatio: '{}',
          AutoGroups: '[]',
          GroupDisplayConfig: JSON.stringify({
            categories: [
              { id: 'other', name: 'Other groups', icon: '', order: 0 },
            ],
            assignments: {
              gpt: { category_id: 'other', order: 0 },
            },
          }),
        }}
      />
    )

    await user.click(screen.getByRole('tab', { name: 'Group display' }))
    await user.click(
      screen.getByRole('button', { name: 'Reset to automatic categories' })
    )
    const dialog = screen.getByRole('alertdialog')
    expect(within(dialog).getByText('Reset display categories?')).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: 'Reset' }))

    expect(screen.getByRole('region', { name: 'GPT' })).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Reset to automatic categories' })
    ).toBeDisabled()
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].GroupDisplayConfig)).toEqual({
      categories: [],
      assignments: {},
    })
  })

  it('shows inherited groups and moves one directly into a custom category', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          GroupRatio: '{"gpt-pro":1,"gpt-new":1}',
          UserUsableGroups:
            '{"gpt-pro":"Stable GPT pool","gpt-new":"New GPT pool"}',
          TopupGroupRatio: '{}',
          AutoGroups: '[]',
          GroupDisplayConfig: JSON.stringify({
            categories: [
              {
                id: 'provider:openai',
                name: 'GPT Zone',
                icon: 'OpenAI.Color',
                order: 0,
              },
              {
                id: 'display_custom',
                name: 'Custom',
                icon: '',
                order: 10,
              },
            ],
            assignments: {
              'gpt-pro': { category_id: 'provider:openai', order: 0 },
            },
          }),
        }}
      />
    )

    await user.click(screen.getByRole('tab', { name: 'Group display' }))
    const gptCategory = screen.getByRole('region', { name: 'GPT Zone' })
    const customCategory = screen.getByRole('region', { name: 'Custom' })
    const list = within(gptCategory).getByRole('list', {
      name: 'Model groups in this category',
    })
    const rows = within(list).getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('gpt-pro')
    expect(rows[1]).toHaveTextContent('gpt-new')
    expect(within(rows[1]).getByText('Auto')).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Remove gpt-new' })).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Restore automatic category' })
    ).toBeVisible()

    await user.click(
      within(rows[1]).getByRole('combobox', {
        name: 'Move gpt-new to category',
      })
    )
    await user.click(screen.getByRole('option', { name: 'Custom' }))

    expect(within(gptCategory).queryByText('gpt-new')).toBeNull()
    const customList = within(customCategory).getByRole('list', {
      name: 'Model groups in this category',
    })
    expect(within(customList).getByText('gpt-new')).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].GroupDisplayConfig)).toEqual({
      categories: [
        {
          id: 'provider:openai',
          name: 'GPT Zone',
          icon: 'OpenAI.Color',
          order: 0,
        },
        {
          id: 'display_custom',
          name: 'Custom',
          icon: '',
          order: 10,
        },
      ],
      assignments: {
        'gpt-pro': { category_id: 'provider:openai', order: 0 },
        'gpt-new': { category_id: 'display_custom', order: 0 },
      },
    })
  })

  it('reorders model groups inside a display category and saves their order', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          GroupDisplayConfig: JSON.stringify({
            categories: [{ id: 'openai', name: 'GPT', icon: '', order: 0 }],
            assignments: {
              default: { category_id: 'openai', order: 0 },
              vip: { category_id: 'openai', order: 10 },
            },
          }),
        }}
      />
    )

    await user.click(screen.getByRole('tab', { name: 'Group display' }))
    const list = screen.getByRole('list', {
      name: 'Model groups in this category',
    })
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent(
      'default'
    )
    await user.click(screen.getByRole('button', { name: 'Move vip up' }))
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent('vip')

    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].GroupDisplayConfig)).toEqual({
      categories: [{ id: 'openai', name: 'GPT', icon: '', order: 0 }],
      assignments: {
        vip: { category_id: 'openai', order: 0 },
        default: { category_id: 'openai', order: 10 },
      },
    })
  })

  it('reorders from the drag handle with arrow keys, preserves unknown groups and saves the new order', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture
        onSave={onSave}
        initial={{
          AutoGroups: '["default","vip","retired"]',
          MaxTokenAutoGroups: 1,
        }}
      />
    )
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const list = screen.getByRole('list', { name: 'Auto group order' })
    const handle = screen.getByRole('button', {
      name: 'Drag default to reorder',
    })
    handle.focus()
    await user.keyboard('{ArrowUp}')
    expect(within(list).getAllByRole('listitem')[0]).toHaveTextContent(
      'default'
    )
    await user.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}')
    expect(within(list).getAllByRole('listitem')[2]).toHaveTextContent(
      'default'
    )
    expect(handle).toHaveFocus()
    await user.keyboard('{ArrowUp}')
    expect(within(list).getAllByRole('listitem')[1]).toHaveTextContent(
      'default'
    )
    expect(screen.getByText('Not in pricing table')).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(JSON.parse(onSave.mock.calls[0][0].AutoGroups)).toEqual([
      'vip',
      'default',
      'retired',
    ])
  })

  it('keeps a single auto group unchanged when using its drag handle keyboard controls', async () => {
    const user = userEvent.setup()
    render(<Fixture initial={{ AutoGroups: '["default"]' }} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    const handle = screen.getByRole('button', {
      name: 'Drag default to reorder',
    })
    handle.focus()
    await user.keyboard('{ArrowUp}{ArrowDown}')
    const list = screen.getByRole('list', { name: 'Auto group order' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(1)
    expect(within(list).getByRole('listitem')).toHaveTextContent('default')
    expect(handle).toHaveFocus()
    expect(
      screen.getByRole('button', { name: 'Move default up' })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Move default down' })
    ).toBeDisabled()
  })

  it('opens the auto section and focuses its invalid limit when saving from another section', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    fireEvent.change(
      screen.getByRole('spinbutton', {
        name: 'Maximum custom groups per token',
      }),
      { target: { value: '0' } }
    )
    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() =>
      expect(
        screen.getByRole('tab', { name: 'Auto group order' })
      ).toHaveAttribute('aria-selected', 'true')
    )
    const input = screen.getByRole('spinbutton', {
      name: 'Maximum custom groups per token',
    })
    expect(input).toHaveAttribute('aria-invalid', 'true')
    await waitFor(() => expect(input).toHaveFocus())
    expect(screen.getByText('Enter a positive integer')).toBeVisible()
    expect(onSave).not.toHaveBeenCalled()
  })

  it('shows empty state guidance and disables save during a pending request', async () => {
    const user = userEvent.setup()
    render(
      <Fixture
        isSaving
        initial={{
          GroupRatio: '{}',
          TopupGroupRatio: '{}',
          UserUsableGroups: '{}',
          AutoGroups: '[]',
        }}
      />
    )
    expect(
      screen.getByText('No groups yet. Add a group to get started.')
    ).toBeVisible()
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    await user.click(screen.getByRole('tab', { name: 'Special ratio rules' }))
    expect(
      screen.getByText('Base group ratios apply until you add an override.')
    ).toBeVisible()
    await user.click(screen.getByRole('tab', { name: 'Auto group order' }))
    expect(screen.getByText('No auto groups configured')).toBeVisible()
  })

  it('preserves pricing edits when switching between visual and JSON editors', async () => {
    const user = userEvent.setup()
    render(<Fixture />)
    await user.clear(
      screen.getAllByRole('textbox', { name: 'Group description' })[1]
    )
    await user.type(
      screen.getAllByRole('textbox', { name: 'Group description' })[1],
      'Updated description'
    )
    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
    await user.click(screen.getByRole('button', { name: 'Switch to Visual' }))
    expect(
      screen.getAllByRole('textbox', { name: 'Group description' })[1]
    ).toHaveValue('Updated description')
  })
})
