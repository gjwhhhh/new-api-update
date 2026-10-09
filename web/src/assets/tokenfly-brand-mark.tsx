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
import type { SVGProps } from 'react'

import { cn } from '@/lib/utils'

export function TokenflyBrandMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg
      {...props}
      className={cn('tokenfly-brand-mark', props.className)}
      viewBox='0 0 32 32'
      fill='none'
      xmlns='http://www.w3.org/2000/svg'
      focusable='false'
    >
      <rect x='2' y='2' width='28' height='28' rx='9' fill='currentColor' />
      <ellipse
        cx='16'
        cy='16'
        rx='10.5'
        ry='4.35'
        transform='rotate(-28 16 16)'
        stroke='var(--background)'
        strokeWidth='1.05'
        opacity='0.42'
      />
      <path
        d='M16 7.1C16.7 11.9 20.1 15.3 24.9 16C20.1 16.7 16.7 20.1 16 24.9C15.3 20.1 11.9 16.7 7.1 16C11.9 15.3 15.3 11.9 16 7.1Z'
        fill='var(--background)'
      />
      <circle
        cx='24.4'
        cy='10.75'
        r='1.65'
        fill='#d4d4d8'
        stroke='currentColor'
        strokeWidth='0.9'
      />
    </svg>
  )
}
