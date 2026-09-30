#!/usr/bin/env bash
set -euo pipefail
# 回退本次收尾：恢复旧节点运行与已交接记录的归属，公网入口仍保持新版。
python3 - "${1:-}" <<'PY'
import json,pathlib,subprocess,sys,time
B=pathlib.Path('/opt/new-api/backups/cleanup-20260930-green')
OLD='new-api-node-green-20260922'
NEW='new-api-node-blue-20260930'
rows=json.loads((B/'handoff.before.json').read_text())
ids=','.join(str(int(r['id'])) for r in rows) or 'NULL'
def command(args):
    return subprocess.check_output(args,text=True).strip()
def sql(query):
    return command(['docker','exec','new-api-postgres','psql','-X','-q','-v','ON_ERROR_STOP=1','-U','root','-d','new-api','-Atc',query])
if len(sys.argv)>1 and sys.argv[1]=='--verify-copy':
    # 在隔离临时表复制真实归属记录，验证恢复只改变 node_id，不影响其他数据。
    values=','.join("(%d,'%s','%s',%s)" % (int(r['id']),NEW,r['status'],'NULL' if r['result_expired_at'] is None else int(r['result_expired_at'])) for r in rows)
    insert=f'INSERT INTO cleanup_copy VALUES {values};' if rows else ''
    restored=json.loads(sql(f"""BEGIN;
CREATE TEMP TABLE cleanup_copy(id bigint,node_id text,status text,result_expired_at bigint);
{insert}
UPDATE cleanup_copy SET node_id='{OLD}' WHERE id IN ({ids}) AND node_id='{NEW}';
SELECT coalesce(json_agg(t ORDER BY id),'[]'::json) FROM cleanup_copy t;
ROLLBACK;"""))
    assert restored==rows
    (B/'rollback-copy.json').write_text(json.dumps(restored,sort_keys=True)+'\n')
    print('ROLLBACK_COPY=PASS ROWS='+str(len(restored))+' OTHER_FIELDS_UNCHANGED=PASS')
    sys.exit(0)
# 防止在后续部署上使用过期脚本。
assert pathlib.Path('/opt/new-api/docker-compose.yml').read_bytes()==(B/'compose.before.yml').read_bytes()
assert pathlib.Path('/opt/new-api/caddy/Caddyfile').read_bytes()==(B/'Caddyfile.before').read_bytes()
baseline=json.loads((B/'BASELINE_FILE.json').read_text())
assert command(['docker','inspect','-f','{{.Image}}','new-api-green-20260922'])==baseline['old']['image']
command(['docker','start','new-api-green-20260922'])
for attempt in range(40):
    health=command(['docker','inspect','-f','{{.State.Health.Status}}','new-api-green-20260922'])
    if health=='healthy': break
    time.sleep(1)
assert health=='healthy'
# 已删除或已被其他维护过程移交的行不回写；不恢复媒体文件或生成请求。
count=sql(f"""BEGIN;
SET LOCAL lock_timeout='5s';
WITH restored AS (
 UPDATE async_relay_tasks SET node_id='{OLD}'
 WHERE id IN ({ids}) AND node_id='{NEW}' AND status IN ('succeeded','failed','cancelled')
 RETURNING id
) SELECT count(*) FROM restored;
COMMIT;""")
print('ROLLBACK=PASS OLD_RUNNING=TRUE PUBLIC_ROUTING=UNCHANGED RESTORED_ROWS='+count)
PY
