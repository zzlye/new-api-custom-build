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
import { describe, expect, test } from 'vitest'

import type { SystemStatus } from '@/features/auth/types'
import { generateAffiliateLink } from '@/features/wallet/lib/affiliate'

import { buildOAuthAuthorizationUrl } from '../oauth'
import { publicServerAddress } from '../public-address'

describe('公开站点地址', () => {
  test('推广链接跟随服务器地址且保留端口前缀，不把邀请码拼成额外参数', () => {
    const base = publicServerAddress(
      { server_address: 'https://public.example:8443/root/' },
      'https://old.example'
    )
    expect(generateAffiliateLink('code&next=bad', base)).toBe(
      'https://public.example:8443/root/sign-up?aff=code%26next%3Dbad'
    )
    expect(publicServerAddress(null, 'https://old.example/')).toBe(
      'https://old.example'
    )
  })
  test.each(['discord', 'oidc', 'linuxdo', 'custom'])(
    '%s 授权和后端使用相同配置回调地址',
    (provider) => {
      const status = {
        server_address: 'https://public.example:8443/root/',
        discord_client_id: 'fixture',
        linuxdo_client_id: 'fixture',
        oidc_client_id: 'fixture',
        oidc_authorization_endpoint: 'https://identity.example/authorize',
        custom_oauth_providers: [
          {
            id: 1,
            name: 'Custom',
            icon: '',
            scopes: 'openid',
            slug: 'custom',
            client_id: 'fixture',
            authorization_endpoint: 'https://identity.example/authorize',
          },
        ],
      } as SystemStatus
      for (const base of [
        'https://public.example:8443/root/',
        'https://changed.example/',
      ]) {
        status.server_address = base
        const url = new URL(
          buildOAuthAuthorizationUrl(provider, 'fixture-state', status)
        )
        expect(url.searchParams.get('redirect_uri')).toBe(
          `${base.replace(/\/+$/, '')}/oauth/${provider}`
        )
        expect(url.searchParams.get('state')).toBe('fixture-state')
      }
    }
  )
})
