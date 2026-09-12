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
import { Link } from '@tanstack/react-router'
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { formatPrice, formatRequestPrice } from '@/features/pricing/lib/price'

export function ModelCatalog() {
  const { t } = useTranslation()
  const { models, isLoading, error, refetch } = usePricingData()

  if (isLoading) {
    return (
      <div
        className='brand-model-list'
        aria-label={t('Loading...')}
        aria-busy='true'
      >
        {[0, 1, 2].map((key) => (
          <Skeleton key={key} className='my-4 h-20 w-full' />
        ))}
      </div>
    )
  }
  if (error) {
    return (
      <div className='brand-catalog-message' role='status'>
        <p>{t('Model pricing is temporarily unavailable.')}</p>
        <Button variant='outline' onClick={() => void refetch()}>
          {t('Retry')}
        </Button>
      </div>
    )
  }
  if (models.length === 0) {
    return (
      <div className='brand-catalog-message'>
        <div>
          <h3>{t('Your model catalog starts here.')}</h3>
          <p>
            {t(
              'No models are published yet. Available models and prices will appear after configuration.'
            )}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className='brand-model-list'>
      {models.slice(0, 4).map((model) => {
        let price = t('View Pricing')
        let unit = ''
        if (model.billing_mode === 'tiered_expr') price = t('Dynamic Pricing')
        else if (model.quota_type === 1 && Number.isFinite(model.model_price)) {
          price = formatRequestPrice(model)
          unit = `/ ${t('request')}`
        } else if (
          model.quota_type === 0 &&
          Number.isFinite(model.model_ratio)
        ) {
          price = formatPrice(model, 'input', 'M')
          unit = `${t('Input')} / 1M tokens`
        }
        return (
          <Link
            key={model.model_name}
            to='/pricing/$modelId'
            params={{ modelId: model.model_name }}
            className='brand-model-row'
          >
            <span className='brand-model-initial' aria-hidden='true'>
              {model.model_name.charAt(0).toUpperCase()}
            </span>
            <div className='brand-model-name'>
              <h3>{model.model_name}</h3>
              <p>
                {model.description ||
                  model.vendor_name ||
                  t('View model details')}
              </p>
            </div>
            <div className='brand-model-price'>
              <strong>{price}</strong>
              <span>{unit}</span>
            </div>
            <ArrowUpRight size={20} aria-hidden='true' />
          </Link>
        )
      })}
      <p className='brand-caption'>
        {t(
          'Starting prices across configured groups. See model details for output, cache, tiered pricing, and applicable groups.'
        )}
      </p>
    </div>
  )
}
