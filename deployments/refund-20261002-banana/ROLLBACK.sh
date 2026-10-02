#!/usr/bin/env bash
set -euo pipefail
# 仅在隔离临时表恢复原状态，真实客户补偿保持到账。
python3 /opt/new-api/backups/refund-banana-20261002/REFUND.py test
