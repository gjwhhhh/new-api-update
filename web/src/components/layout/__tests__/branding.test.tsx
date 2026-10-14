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
import { cleanup, render, screen, within } from '@testing-library/react'
import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { PublicLayout } from '@/components/layout/components/public-layout'
import { SystemBrand } from '@/components/layout/components/system-brand'

const logo = '/brand/tokenflyapi-mark.svg'

vi.mock('@tanstack/react-router', () => ({
  Link: (
    props: AnchorHTMLAttributes<HTMLAnchorElement> & {
      children: ReactNode
      to: string
    }
  ) => {
    const { children, to, ...anchorProps } = props
    return (
      <a {...anchorProps} href={to}>
        {children}
      </a>
    )
  },
  useNavigate: () => vi.fn(),
  useRouterState: () => ({ location: { pathname: '/' } }),
}))

vi.mock('@/features/system-update/system-update-action', () => ({
  SystemUpdateAction: () => null,
}))

vi.mock('@/hooks/use-notifications', () => ({
  useNotifications: () => ({}),
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: { system_name: 'tokenflyapi', version: 'test' },
  }),
}))

vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({
    systemName: 'tokenflyapi',
    logo,
    loading: false,
    logoLoaded: true,
  }),
}))

vi.mock('@/hooks/use-top-nav-links', () => ({
  useTopNavLinks: () => [],
}))

afterEach(cleanup)

describe('shared system branding', () => {
  test('public pages render the configured logo instead of a hard-coded mark', () => {
    const result = render(
      <PublicLayout
        showMainContainer={false}
        showAuthButtons={false}
        showNotifications={false}
        showThemeSwitch={false}
        navLinks={[]}
      >
        <main>Public content</main>
      </PublicLayout>
    )

    const brand = screen.getByRole('link', { name: 'logo tokenflyapi' })
    expect(within(brand).getByRole('img', { name: 'logo' })).toHaveAttribute(
      'src',
      logo
    )
    expect(result.container.querySelector('.tokenfly-brand-mark')).toBeNull()
  })

  test('console header renders the same configured logo', () => {
    const result = render(<SystemBrand variant='inline' />)

    const brand = screen.getByRole('link', { name: 'Go to home' })
    expect(brand.querySelector('img')).toHaveAttribute('src', logo)
    expect(result.container.querySelector('.tokenfly-brand-mark')).toBeNull()
  })
})
