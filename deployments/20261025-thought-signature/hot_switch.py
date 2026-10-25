import copy
import hashlib
import json
import pathlib
import re
import socket
import subprocess
import threading
import time
import urllib.error
import urllib.request

# 本次仅更新后端；前端继续使用当前线上保留目录，旧进程保持运行以完成已接收任务。
MODE = globals().get('MODE', 'prepare')
ROOT = pathlib.Path('/opt/new-api')
BACK = ROOT / 'backups/thought-signature-dd2a1c997-20261025'
CONFIG = ROOT / 'caddy/Caddyfile'
COMMIT = 'dd2a1c997'
OLD_SERVICE = 'new-api-next-baa390d9a'
SERVICE = 'new-api-next-' + COMMIT
CANDIDATE = 'new-api-candidate-' + COMMIT
PREVIOUS = 'new-api-previous-baa390d9a'
PORT = 3005


def run(args):
    return subprocess.check_output(args, text=True).strip()


def inspect(name):
    return json.loads(run(['docker', 'inspect', name]))[0]


def sql(query):
    return run(['docker', 'exec', 'new-api-postgres', 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', 'root', '-d', 'new-api', '-Atc', query])


def write(path, content):
    path.write_text(content, encoding='utf-8')
    path.chmod(0o600)


def get(url):
    with urllib.request.urlopen(url, timeout=25) as response:
        return response.read()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def health(base):
    value = json.loads(get(base + '/api/status'))
    expected = json.loads((BACK / 'baseline.json').read_text())['version']
    assert value['success'] and value['data']['version'] == expected, ('health/version', base)


def auth(base):
    for path in ['/api/user/self', '/api/channel/', '/dashboard/billing/subscription', '/api/option/']:
        try:
            response = urllib.request.urlopen(base + path, timeout=15)
        except urllib.error.HTTPError as error:
            assert error.code in (401, 403), (path, error.code)
        else:
            value = json.loads(response.read())
            assert value.get('success') is False or value.get('error'), ('authentication bypass', path)


def verify(base):
    health(base)
    auth(base)
    baseline = json.loads((BACK / 'baseline.json').read_text())
    for path in ['/', '/register', '/wallet', '/system-settings/request-policies/parameters', '/lottery']:
        assert sha(get(base + path)) == baseline['html_sha256'], ('frontend entry changed', base, path)
    assert sha(get(base + baseline['main'])) == baseline['main_sha256'], ('frontend script changed', base)
    try:
        response = urllib.request.urlopen(base + '/task-media/nonexistent/audio/0', timeout=15)
    except urllib.error.HTTPError as error:
        assert error.code in (400, 401, 403, 404)
    else:
        assert 'text/html' not in response.headers.get('Content-Type', ''), 'media route intercepted'


def reload_config(content):
    previous = CONFIG.read_text()
    # 原地写入以保持文件挂载，验证失败立即回退。
    CONFIG.write_text(content)
    try:
        for action in ['validate', 'reload']:
            subprocess.run(['docker', 'exec', 'new-api-caddy', 'caddy', action, '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    except Exception:
        CONFIG.write_text(previous)
        subprocess.run(['docker', 'exec', 'new-api-caddy', 'caddy', 'reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        raise


if MODE == 'prepare':
    BACK.mkdir(mode=0o700)
    write(BACK / 'compose.before.json', (ROOT / 'compose.json').read_text())
    write(BACK / 'Caddyfile.before', CONFIG.read_text())
    snapshots = [inspect(name) for name in run(['docker', 'ps', '--format', '{{.Names}}']).splitlines()]
    write(BACK / 'containers.before.json', json.dumps(snapshots))
    html = get('http://127.0.0.1:3000/')
    main = next(path for path in re.findall(r'src="([^"]+\.js)"', html.decode()) if '/index.' in path)
    status = json.loads(get('http://127.0.0.1:3000/api/status'))
    assert status['success']
    write(BACK / 'baseline.json', json.dumps({'version': status['data']['version'], 'html_sha256': sha(html), 'main': main, 'main_sha256': sha(get('http://127.0.0.1:3000' + main))}))
    write(BACK / 'options.before.json', sql("select coalesce(json_object_agg(key,md5(value)),'{}'::json) from options"))
    cfg = json.loads((BACK / 'compose.before.json').read_text())
    assert cfg['services'][OLD_SERVICE]['container_name'] == 'new-api'
    assert cfg['services'][OLD_SERVICE]['ports'] == ['127.0.0.1:3004:3000']
    # 数据库一致性备份存数据盘，校验归档目录后才允许创建候选容器。
    directory = pathlib.Path('/mnt/data200/new-api/backups/thought-signature-dd2a1c997-20261025')
    directory.mkdir(mode=0o700, parents=True)
    dump = directory / 'database.before.dump'
    partial = directory / 'database.partial'
    with partial.open('xb') as output:
        partial.chmod(0o600)
        subprocess.run(['docker', 'exec', 'new-api-postgres', 'pg_dump', '-U', 'root', '-d', 'new-api', '-Fc'], check=True, stdout=output)
    with partial.open('rb') as source:
        subprocess.run(['docker', 'exec', '-i', 'new-api-postgres', 'pg_restore', '--list'], check=True, stdin=source, stdout=subprocess.DEVNULL)
    partial.rename(dump)
    write(BACK / 'prepared.json', json.dumps({'database': str(dump), 'bytes': dump.stat().st_size}))
    print('PREPARED', dump.stat().st_size, flush=True)
elif MODE == 'candidate':
    assert (BACK / 'prepared.json').exists()
    assert CONFIG.read_text() == (BACK / 'Caddyfile.before').read_text()
    assert (ROOT / 'compose.json').read_text() == (BACK / 'compose.before.json').read_text()
    release = json.loads((BACK / 'release.json').read_text())
    assert release['commit'].startswith(COMMIT)
    image = release['image']
    assert re.fullmatch(r'ghcr\.io/zzlye/new-api-custom-build@sha256:[a-f0-9]{64}', image)
    smoke = json.loads((BACK / 'image-test.json').read_text())
    assert smoke['passed'] and smoke['image'] == image, 'image behavior not verified'
    subprocess.run(['docker', 'pull', image], check=True)
    image_id = json.loads(run(['docker', 'image', 'inspect', image]))[0]['Id']
    names = run(['docker', 'ps', '-a', '--format', '{{.Names}}']).splitlines()
    assert CANDIDATE not in names and PREVIOUS not in names
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', PORT))
    cfg = json.loads((BACK / 'compose.before.json').read_text())
    service = copy.deepcopy(cfg['services'][OLD_SERVICE])
    service.update(image=image, container_name=CANDIDATE, ports=[f'127.0.0.1:{PORT}:3000'])
    service['environment']['NODE_NAME'] = 'new-api-node-20261025-' + COMMIT
    cfg['services'][SERVICE] = service
    write(BACK / 'candidate.json', json.dumps(cfg, indent=2))
    run(['docker', 'compose', '-p', 'new-api', '-f', str(BACK / 'candidate.json'), 'config', '--quiet'])
    subprocess.run(['docker', 'compose', '-p', 'new-api', '-f', str(BACK / 'candidate.json'), 'up', '-d', '--no-deps', SERVICE], check=True)
    for _ in range(90):
        candidate = inspect(CANDIDATE)
        if candidate['State'].get('Health', {}).get('Status') == 'healthy':
            break
        assert candidate['State']['Running'], 'candidate stopped'
        time.sleep(2)
    assert candidate['State'].get('Health', {}).get('Status') == 'healthy'
    assert candidate['Image'] == image_id
    health(f'http://127.0.0.1:{PORT}')
    auth(f'http://127.0.0.1:{PORT}')
    old = CONFIG.read_text()
    assert old.count('reverse_proxy 127.0.0.1:3004') == 2
    final = old.replace('reverse_proxy 127.0.0.1:3004', f'reverse_proxy 127.0.0.1:{PORT}')
    write(BACK / 'Caddyfile.after', final)
    verify('http://127.0.0.1:3000')
    write(BACK / 'ready.json', json.dumps({'image': image, 'image_id': image_id}))
    print('READY; live traffic unchanged', flush=True)
elif MODE == 'switch':
    assert (BACK / 'ready.json').exists()
    assert CONFIG.read_text() == (BACK / 'Caddyfile.before').read_text()
    health(f'http://127.0.0.1:{PORT}')
    stop = threading.Event()
    monitor = {'samples': 0, 'failures': 0}
    def sample():
        while not stop.is_set():
            try:
                health('http://127.0.0.1:3000')
            except Exception as error:
                monitor['failures'] += 1
                monitor['last_error'] = type(error).__name__
            monitor['samples'] += 1
            stop.wait(0.5)
    thread = threading.Thread(target=sample)
    thread.start()
    try:
        reload_config((BACK / 'Caddyfile.after').read_text())
        for base in ['http://127.0.0.1:3000', 'https://api.zzlye.xyz', 'https://api.zzlye.site', 'https://api.xn--v5z30ywuk.com']:
            verify(base)
        assert monitor['failures'] == 0, monitor
        write(BACK / 'switched.json', json.dumps({'at': int(time.time())}))
    except Exception:
        reload_config((BACK / 'Caddyfile.before').read_text())
        raise
    finally:
        stop.set()
        thread.join()
        write(BACK / 'monitor.json', json.dumps(monitor))
    print('SWITCHED', json.dumps(monitor), flush=True)
elif MODE == 'finalize':
    assert (BACK / 'switched.json').exists()
    assert CONFIG.read_text() == (BACK / 'Caddyfile.after').read_text()
    ready = json.loads((BACK / 'ready.json').read_text())
    names = run(['docker', 'ps', '-a', '--format', '{{.Names}}']).splitlines()
    if CANDIDATE in names:
        assert PREVIOUS not in names
        run(['docker', 'rename', 'new-api', PREVIOUS])
        try:
            run(['docker', 'rename', CANDIDATE, 'new-api'])
        except Exception:
            run(['docker', 'rename', PREVIOUS, 'new-api'])
            raise
    assert inspect('new-api')['Image'] == ready['image_id']
    run(['docker', 'exec', 'wenyun-workshop', 'node', '-e', "fetch('http://new-api:3000/api/status').then(r=>r.json()).then(j=>{if(!j.success||j.data.version!=='')process.exit(1);console.log('internal OK')}).catch(()=>process.exit(1))"])
    cfg = json.loads((BACK / 'compose.before.json').read_text())
    service = json.loads((BACK / 'candidate.json').read_text())['services'][SERVICE]
    service['container_name'] = 'new-api'
    cfg['services'].pop(OLD_SERVICE)
    cfg['services'][SERVICE] = service
    for other in cfg['services'].values():
        dependencies = other.get('depends_on')
        if isinstance(dependencies, list):
            other['depends_on'] = [SERVICE if item == OLD_SERVICE else item for item in dependencies]
        elif isinstance(dependencies, dict) and OLD_SERVICE in dependencies:
            dependencies[SERVICE] = dependencies.pop(OLD_SERVICE)
    write(BACK / 'compose.after.json', json.dumps(cfg, indent=2))
    run(['docker', 'compose', '-p', 'new-api', '-f', str(BACK / 'compose.after.json'), 'config', '--quiet'])
    write(ROOT / 'compose.json', (BACK / 'compose.after.json').read_text())
    verify('http://127.0.0.1:3000')
    verify('https://api.zzlye.xyz')
    before = json.loads((BACK / 'options.before.json').read_text())
    after = json.loads(sql("select coalesce(json_object_agg(key,md5(value)),'{}'::json) from options"))
    changes = [key for key in set(before) | set(after) if before.get(key) != after.get(key)]
    for saved in json.loads((BACK / 'containers.before.json').read_text()):
        live = inspect(saved['Id'])
        assert live['State']['StartedAt'] == saved['State']['StartedAt'], saved['Name']
        assert live['State']['Running'], saved['Name']
    tasks = sql("select node_id,status,count(*) from async_relay_tasks where status not in ('succeeded','failed','cancelled') group by node_id,status")
    result = {'commit': COMMIT, 'image': ready['image'], 'version': json.loads((BACK / 'baseline.json').read_text())['version'], 'frontend_unchanged': True, 'options_changed_since_backup': changes, 'previous': PREVIOUS, 'tasks': tasks, 'completed_at': int(time.time())}
    write(BACK / 'VERIFICATION.json', json.dumps(result, ensure_ascii=False, indent=2))
    print(json.dumps(result, ensure_ascii=False), flush=True)
elif MODE == 'rollback':
    # 回退不恢复数据库快照，避免丢失切换后产生的真实业务数据。
    health('http://127.0.0.1:3004')
    reload_config((BACK / 'Caddyfile.before').read_text())
    names = run(['docker', 'ps', '-a', '--format', '{{.Names}}']).splitlines()
    if PREVIOUS in names:
        if 'new-api' in names:
            run(['docker', 'rename', 'new-api', CANDIDATE])
        run(['docker', 'rename', PREVIOUS, 'new-api'])
    write(ROOT / 'compose.json', (BACK / 'compose.before.json').read_text())
    print('ROLLED BACK; task containers retained', flush=True)
else:
    raise ValueError(MODE)
