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
import { z } from 'zod'

export const mailComposeSchema = z.object({
  subject: z
    .string()
    .trim()
    .min(1, 'Subject is required')
    .max(200, 'Subject must not exceed 200 characters')
    .refine(
      (value) =>
        ![...value].some(
          (character) =>
            character.charCodeAt(0) < 32 ||
            (character.charCodeAt(0) >= 127 && character.charCodeAt(0) <= 159)
        ),
      'Subject must not contain line breaks'
    ),
  body: z
    .string()
    .min(1, 'Email body is required')
    .refine((value) => value.trim().length > 0, 'Email body is required')
    .refine(
      (value) => new TextEncoder().encode(value).length <= 256 * 1024,
      'Email body must not exceed 256 KiB'
    ),
})
export type MailComposeValues = z.infer<typeof mailComposeSchema>
