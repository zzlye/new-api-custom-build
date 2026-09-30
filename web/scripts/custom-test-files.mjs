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
import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = fileURLToPath(new URL('../', import.meta.url))

// 原有定制用例依赖 Bun 的模块模拟与独立 DOM，单独运行以免污染官方用例。
export const customTestFiles = readdirSync(path.join(webRoot, 'src'), {
  recursive: true,
})
  .filter((name) => /\.test\.tsx?$/.test(name))
  .map((name) => `src/${name.replaceAll('\\', '/')}`)
  .filter((name) =>
    /from ['"]node:test['"]/.test(
      readFileSync(path.join(webRoot, name), 'utf8')
    )
  )
  .sort()
