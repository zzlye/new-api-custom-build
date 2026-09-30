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
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

import { customTestFiles } from './custom-test-files.mjs'

const webRoot = fileURLToPath(new URL('../', import.meta.url))
let failed = 0

for (const file of customTestFiles) {
  // 每个文件使用独立进程，保证模块模拟和浏览器全局变量不会跨文件残留。
  const result = spawnSync(process.execPath, ['test', file], {
    cwd: webRoot,
    stdio: 'inherit',
  })
  if (result.error || result.status !== 0) {
    failed += 1
  }
}

console.log(`定制测试：${customTestFiles.length - failed} 通过，${failed} 失败`)
process.exitCode = failed ? 1 : 0
