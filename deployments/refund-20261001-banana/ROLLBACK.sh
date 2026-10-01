#!/usr/bin/env bash
set -euo pipefail
# 仅在隔离临时表验证反向恢复，不从客户真实余额重新扣钱。
python3 /opt/new-api/backups/refund-banana-20261001-before1730/REFUND.py test
