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
BACK = pathlib.Path('/opt/new-api/backups/tool-pairing-ffec8e619-20261025')
image = json.loads((BACK / 'release.json').read_text())['image']
directory = pathlib.Path('/mnt/data200/new-api/backups/tool-pairing-ffec8e619-20261025/image-test')
directory.mkdir(mode=0o700, exist_ok=True)
directory = pathlib.Path(tempfile.mkdtemp(prefix='run-', dir=directory))
name = 'new-api-tool-pairing-check-ffec8e619'


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
    # 对比原生工具轮次和连续的 OpenAI tool 消息，覆盖合法乱序与同数量但错 ID。
    native_calls = {'role': 'model', 'parts': [{'functionCall': {'id': 'a', 'name': 'f'}}, {'functionCall': {'id': 'b', 'name': 'g'}}]}
    native_response_a = {'functionResponse': {'id': 'a', 'name': 'f', 'response': {'result': 'ok'}}}
    native_response_b = {'functionResponse': {'id': 'b', 'name': 'g', 'response': {'result': 'ok'}}}
    chat_calls = {'role': 'assistant', 'tool_calls': [{'id': 'a', 'type': 'function', 'function': {'name': 'f', 'arguments': '{}'}}, {'id': 'b', 'type': 'function', 'function': {'name': 'g', 'arguments': '{}'}}]}
    chat_response_a = {'role': 'tool', 'tool_call_id': 'a', 'content': 'ok'}
    chat_response_b = {'role': 'tool', 'tool_call_id': 'b', 'content': 'ok'}
    pairs = [
        ('native-missing', True, {'contents': [native_calls]}, True),
        ('native-short', True, {'contents': [native_calls, {'role': 'user', 'parts': [native_response_a]}]}, True),
        ('native-duplicate', True, {'contents': [native_calls, {'role': 'user', 'parts': [native_response_a, native_response_a]}]}, True),
        ('native-orphan', True, {'contents': [{'role': 'user', 'parts': [native_response_a]}]}, True),
        ('native-valid-reversed', True, {'contents': [native_calls, {'role': 'user', 'parts': [native_response_b, native_response_a]}]}, False),
        ('chat-short', False, {'model': 'gemini-3.1-pro-preview', 'messages': [chat_calls, chat_response_a]}, True),
        ('chat-duplicate', False, {'model': 'gemini-3.1-pro-preview', 'messages': [chat_calls, chat_response_a, chat_response_a]}, True),
        ('chat-orphan', False, {'model': 'gemini-3.1-pro-preview', 'messages': [chat_response_a]}, True),
        ('chat-valid-reversed', False, {'model': 'gemini-3.1-pro-preview', 'messages': [chat_calls, chat_response_b, chat_response_a]}, False),
    ]
    pairing_logs = 0
    for label, native, payload, blocked in pairs:
        for stream in [False, True]:
            if native:
                path = '/v1beta/models/gemini-3.1-pro-preview:' + ('streamGenerateContent' if stream else 'generateContent')
            else:
                path = '/v1/chat/completions'
                payload['stream'] = stream
            status, value = request(path, payload, keys[0])
            if blocked:
                assert status == 422 and value['error']['code'] == 'local_parameter_error', (label, status, value)
                assert 'function response parts' in value['error']['message'], (label, value)
                pairing_logs += 1
                unselected_status, unselected = request(path, payload, keys[1])
                assert unselected_status == 503 and 'local_parameter_error' not in json.dumps(unselected), (label, unselected)
            else:
                assert status == 503 and 'local_parameter_error' not in json.dumps(value), (label, status, value)
            results.append({'case': label, 'stream': stream, 'blocked': blocked, 'status': status})
    with sqlite3.connect(directory / 'new-api.db') as db:
        assert db.execute('SELECT quota FROM users WHERE id=900001').fetchone()[0] == 123456
        assert db.execute('SELECT count(*) FROM async_relay_tasks').fetchone()[0] == 0
        logs = db.execute("SELECT content,quota,other FROM logs WHERE user_id=900001 AND content LIKE '%Invalid thought signature.%'").fetchall()
        assert len(logs) == 4, len(logs)
        assert all(quota == 0 and 'invalid-signature!' not in content for content, quota, _ in logs)
        actual = db.execute("SELECT count(*) FROM logs WHERE user_id=900001 AND quota=0 AND content LIKE '%function response parts%'").fetchone()[0]
        assert actual == pairing_logs, (actual, pairing_logs)
    result = {'passed': True, 'image': image, 'checks': results, 'group_isolation': True, 'quota_unchanged': True, 'task_count': 0, 'signature_error_logs': len(logs), 'pairing_error_logs': pairing_logs}
    (BACK / 'image-test.json').write_text(json.dumps(result, indent=2))
    print(json.dumps(result), flush=True)
finally:
    # 精确清理本次临时容器，保留测试库和结果用于复核。
    subprocess.run(['docker', 'rm', '-f', container], check=True, stdout=subprocess.DEVNULL)
