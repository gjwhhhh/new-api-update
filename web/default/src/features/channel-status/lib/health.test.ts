import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { shouldShowGroupRatio } from './health'

describe('group ratio display', () => {
  test('preserves valid free and automatic group ratios', () => {
    assert.equal(shouldShowGroupRatio(0), true)
    assert.equal(shouldShowGroupRatio('auto'), true)
  })

  test('hides empty and invalid numeric ratios', () => {
    assert.equal(shouldShowGroupRatio('  '), false)
    assert.equal(shouldShowGroupRatio(Number.NaN), false)
  })
})
