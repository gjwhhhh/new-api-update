/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { z } from 'zod'

export const MIN_USER_CHANNEL_PRIORITY = -1_000_000_000
export const MAX_USER_CHANNEL_PRIORITY = 1_000_000_000

const priorityInputSchema = z.string().superRefine((value, context) => {
  const normalized = value.trim()
  const priority = Number(normalized)
  if (!/^-?\d+$/.test(normalized) || !Number.isSafeInteger(priority)) {
    context.addIssue({ code: 'custom', message: 'Priority must be an integer' })
    return
  }
  if (
    priority < MIN_USER_CHANNEL_PRIORITY ||
    priority > MAX_USER_CHANNEL_PRIORITY
  ) {
    context.addIssue({
      code: 'custom',
      message: 'Priority must be between {{min}} and {{max}}',
    })
  }
})

const draftPrioritySchema = z.object({
  user_id: z.number().int().positive(),
  channel_id: z.number().int().positive(),
  priority: z.number().int(),
  channel_name: z.string(),
  channel_type: z.number().int(),
  channel_status: z.number().int(),
  channel_group: z.string(),
  global_priority: z.number().int(),
  weight: z.number().nonnegative(),
  priorityInput: priorityInputSchema,
})

export const userChannelPriorityFormSchema = z.object({
  items: z.array(draftPrioritySchema),
})

export type DraftUserChannelPriority = z.infer<typeof draftPrioritySchema>
export type UserChannelPriorityFormValues = z.infer<
  typeof userChannelPriorityFormSchema
>
