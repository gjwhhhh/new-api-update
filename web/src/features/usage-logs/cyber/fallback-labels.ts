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
import type { TFunction } from 'i18next'

export function fallbackOutcomeLabel(outcome: string, t: TFunction): string {
  switch (outcome) {
    case 'selected':
      return t('Fallback channel selected')
    case 'submitted':
      return t('Fallback task submitted')
    case 'completed':
      return t('Fallback request completed')
    case 'failed':
      return t('Fallback request failed')
    case 'rejected':
      return t('Fallback routing rejected')
    case 'interrupted':
      return t('Fallback request interrupted')
    default:
      return outcome
  }
}

export function fallbackReasonLabel(reason: string, t: TFunction): string {
  switch (reason) {
    case 'existing_ban':
      return t('Existing Cyber session ban')
    case 'target_rejected':
      return t('Fallback rejected this ban batch')
    case 'same_source':
      return t('Fallback matches the original rejecting channel')
    case 'binding_conflict':
      return t('Fallback conflicts with a required channel binding')
    case 'stateful_context':
      return t('Upstream state cannot migrate to the fallback')
    case 'unpaired_tool_result':
      return t('Tool results require matching tool calls')
    case 'context_unavailable':
      return t('Request context cannot be verified')
    case 'state_unavailable':
      return t('Fallback routing state is temporarily unavailable')
    case 'target_unavailable':
      return t('Fallback is unavailable for this request')
    case 'model_forbidden':
      return t('This token cannot access the requested model')
    case 'reconnect_required':
      return t('Reconnect with independent request context')
    case 'upstream_cyber':
      return t('Fallback upstream rejected the content')
    default:
      return reason
  }
}
