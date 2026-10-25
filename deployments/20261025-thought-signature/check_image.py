import json
import pathlib
import secrets
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

# 使用全新 SQLite 和临时账号验证发布镜像；不连接生产数据库、不配置真实上游。
BACK = pathlib.Path('/opt/new-api/backups/thought-signature-dd2a1c997-20261025')
image = json.loads((BACK / 'release.json').read_text())['image']
directory = pathlib.Path('/mnt/data200/new-api/backups/thought-signature-dd2a1c997-20261025/image-test')
directory.mkdir(mode=0o700, exist_ok=True)
directory = pathlib.Path(tempfile.mkdtemp(prefix='run-', dir=directory))
name = 'new-api-signature-check-dd2a1c997'


def run(args):
    return subprocess.check_output(args, text=True).strip()


def request(path, payload=None, key=None):
    headers = {'Content-Type': 'application/json'}
    if key:
        headers['Authorization'] = 'Bearer sk-' + key
    req = urllib.request.Request(base + path, data=json.dumps(payload).encode() if payload is not None else None, headers=headers)
    try:
        response = urllib.request.urlopen(req, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    return response.code, json.loads(response.read())


def wait_ready():
    for _ in range(60):
        assert json.loads(run(['docker', 'inspect', name]))[0]['State']['Running']
        try:
            status, value = request('/api/status')
            if status == 200 and value['success']:
                return
        except (OSError, ValueError):
            pass
        time.sleep(1)
    raise RuntimeError('isolated container readiness failed')


container = run(['docker', 'run', '-d', '--name', name, '--publish', '127.0.0.1::3000', '--mount', f'type=bind,source={directory},target=/data', '--env', 'SQL_DSN=local', '--env', 'SQLITE_PATH=/data/new-api.db', '--env', 'ERROR_LOG_ENABLED=true', '--env', 'GIN_MODE=release', image])
try:
    base = 'http://' + run(['docker', 'port', name, '3000/tcp'])
    wait_ready()
    # 只暂停本脚本创建的验证容器，再插入不会出现在生产库的测试记录。
    run(['docker', 'stop', container])
    keys = [secrets.token_hex(24), secrets.token_hex(24)]
    policy = {'enabled': True, 'rules': [{'group': 'default', 'enabled': True, 'status_code': 422}], 'check_structure': True, 'check_media': True, 'check_generation_config': True, 'upstream_cooldown_seconds': 15, 'upstream_status_codes': '400'}
    with sqlite3.connect(directory / 'new-api.db') as db:
        for index, group in enumerate(['default', 'unselected']):
            uid = 900001 + index
            db.execute('INSERT INTO users (id,username,password,role,status,quota,"group",setting) VALUES (?,?,?,?,?,?,?,?)', (uid, 'signature-test-' + str(index), 'disabled-login-fixture', 1, 1, 123456, group, '{}'))
            db.execute('INSERT INTO tokens (user_id,key,status,name,expired_time,remain_quota,unlimited_quota,"group") VALUES (?,?,?,?,?,?,?,?)', (uid, keys[index], 1, 'signature-test', -1, 123456, 0, ''))
        db.execute('INSERT OR REPLACE INTO options (key,value) VALUES (?,?)', ('LocalRequestValidationPolicy', json.dumps(policy)))
        assert db.execute('SELECT count(*) FROM channels').fetchone()[0] == 0
    run(['docker', 'start', container])
    # Docker 重启后可能重新分配随机宿主端口，重新读取实际绑定地址。
    base = 'http://' + run(['docker', 'port', name, '3000/tcp'])
    wait_ready()
    results = []
    for action in ['generateContent', 'streamGenerateContent']:
        for signature, blocked in [(123, True), ('invalid-signature!', True), ('YWJjZA==', False), ('context_engineering_is_the_way_to_go', False)]:
            body = {'contents': [{'role': 'model', 'parts': [{'text': 'hello', 'thoughtSignature': signature}]}]}
            status, value = request('/v1beta/models/gemini-3.1-pro-preview:' + action, body, keys[0])
            if blocked:
                assert status == 422 and value['error']['code'] == 'local_parameter_error', (status, value)
                assert 'Invalid thought signature.' in value['error']['message']
                assert 'invalid-signature!' not in value['error']['message']
            else:
                # 验证通过后只能走到无渠道错误，不能误判正常编码为签名错误。
                assert status == 503 and 'local_parameter_error' not in json.dumps(value), (status, value)
            results.append({'action': action, 'blocked': blocked, 'status': status})
    body = {'contents': [{'parts': [{'text': 'hello', 'thoughtSignature': 123}]}]}
    status, value = request('/v1beta/models/gemini-3.1-pro-preview:generateContent', body, keys[1])
    assert status == 503 and 'local_parameter_error' not in json.dumps(value), (status, value)
    with sqlite3.connect(directory / 'new-api.db') as db:
        assert db.execute('SELECT quota FROM users WHERE id=900001').fetchone()[0] == 123456
        assert db.execute('SELECT count(*) FROM async_relay_tasks').fetchone()[0] == 0
        logs = db.execute("SELECT content,quota,other FROM logs WHERE user_id=900001 AND content LIKE '%Invalid thought signature.%'").fetchall()
        assert len(logs) == 4, len(logs)
        assert all(quota == 0 and 'invalid-signature!' not in content for content, quota, _ in logs)
    result = {'passed': True, 'image': image, 'checks': results, 'group_isolation': True, 'quota_unchanged': True, 'task_count': 0, 'signature_error_logs': len(logs)}
    (BACK / 'image-test.json').write_text(json.dumps(result, indent=2))
    print(json.dumps(result), flush=True)
finally:
    # 精确清理本次临时容器，保留测试库和结果用于复核。
    subprocess.run(['docker', 'rm', '-f', container], check=True, stdout=subprocess.DEVNULL)
