import concurrent.futures
import copy
import hashlib
import json
import pathlib
import shutil
import socket
import subprocess
import threading
import time
import urllib.error
import urllib.request

# 每个阶段都校验上一步的实物结果，避免旧发布脚本误切线上服务。
MODE = globals().get('MODE', 'prepare')
ROOT = pathlib.Path('/opt/new-api')
BACK = ROOT / 'backups/parameters-baa390d9a-20261025'
CONFIG = ROOT / 'caddy/Caddyfile'
OLD_PUBLIC = ROOT / 'caddy/data/newapi-preserved-ad8c4fc98'
PUBLIC = ROOT / 'caddy/data/newapi-preserved-baa390d9a'
OLD_SERVICE = 'new-api-next-ad8c4fc98'
SERVICE = 'new-api-next-baa390d9a'
CANDIDATE = 'new-api-candidate-baa390d9a'
PREVIOUS = 'new-api-previous-ad8c4fc98'
NODE = 'new-api-node-20261025-baa390d9a'
MAIN = '/static/js/index.parameters-477101abcccf.js'
MAIN_SHA = '477101abcccf85f8279d74813c9821d5300f8db572d2182c91389874af53d890'
PORT = 3004
PROBE = 'http://127.0.0.1:3003'


def run(args):
    return subprocess.check_output(args, text=True).strip()


def sql(query):
    return run(['docker', 'exec', 'new-api-postgres', 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', 'root', '-d', 'new-api', '-Atc', query])


def inspect(name):
    return json.loads(run(['docker', 'inspect', name]))[0]


def write(path, content):
    path.write_text(content, encoding='utf-8')
    path.chmod(0o600)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def get(url):
    with urllib.request.urlopen(url, timeout=25) as response:
        return response.read()


def health(base):
    value = json.loads(get(base + '/api/status'))
    expected = json.loads((BACK / 'version.json').read_text())['version']
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
    expected = sha((BACK / 'index.after.html').read_bytes())
    for path in ['/', '/register', '/wallet', '/system-settings/request-policies/parameters', '/lottery']:
        assert sha(get(base + path)) == expected, ('frontend entry', base, path)
    assert sha(get(base + MAIN)) == MAIN_SHA, ('frontend script', base)
    try:
        response = urllib.request.urlopen(base + '/task-media/nonexistent/audio/0', timeout=15)
    except urllib.error.HTTPError as error:
        assert error.code in (400, 401, 403, 404)
    else:
        assert 'text/html' not in response.headers.get('Content-Type', ''), 'media route intercepted'


def reload_config(content):
    previous = CONFIG.read_text()
    # 挂载的是配置文件本身，原地写入后验证和重载；失败立即恢复旧配置。
    CONFIG.write_text(content)
    try:
        for action in ['validate', 'reload']:
            subprocess.run(['docker', 'exec', 'new-api-caddy', 'caddy', action, '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    except Exception:
        CONFIG.write_text(previous)
        subprocess.run(['docker', 'exec', 'new-api-caddy', 'caddy', 'reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        raise


def task_state():
    return sql("select node_id,status,count(*) from async_relay_tasks where status not in ('succeeded','failed','cancelled') group by node_id,status")


if MODE == 'prepare':
    assert CONFIG.read_bytes() == (BACK / 'Caddyfile.before').read_bytes(), 'live proxy changed'
    assert (ROOT / 'compose.json').read_bytes() == (BACK / 'compose.before.json').read_bytes(), 'live compose changed'
    old = json.loads((BACK / 'container.before.json').read_text())
    assert inspect('new-api')['Id'] == old['Id'], 'live container changed'
    names = run(['docker', 'ps', '--format', '{{.Names}}']).splitlines()
    if not (BACK / 'containers.before.json').exists():
        snapshots = [inspect(name) for name in names]
        write(BACK / 'containers.before.json', json.dumps(snapshots))
    # 数据库备份放数据盘，既不占满系统盘，也不暂停线上业务。
    dbdir = pathlib.Path('/mnt/data200/new-api/backups/parameters-baa390d9a-20261025')
    dbdir.mkdir(mode=0o700, parents=True, exist_ok=True)
    db = dbdir / 'database.before.dump'
    if not db.exists():
        partial = db.with_suffix('.partial')
        with partial.open('xb') as output:
            partial.chmod(0o600)
            subprocess.run(['docker', 'exec', 'new-api-postgres', 'pg_dump', '-U', 'root', '-d', 'new-api', '-Fc'], check=True, stdout=output)
        with partial.open('rb') as source:
            subprocess.run(['docker', 'exec', '-i', 'new-api-postgres', 'pg_restore', '--list'], stdin=source, stdout=subprocess.DEVNULL, check=True)
        partial.rename(db)
    assert db.stat().st_size > 0
    write(BACK / 'database-backup.json', json.dumps({'path': str(db), 'bytes': db.stat().st_size}))
    assert sha((BACK / 'main.after.js').read_bytes()) == MAIN_SHA
    expected_html = (BACK / 'index.after.html').read_bytes()
    old_main = json.loads((BACK / 'assets.json').read_text())['main']
    assert expected_html == (BACK / 'index.before.html').read_bytes().replace(old_main.encode(), MAIN.encode())
    if not PUBLIC.exists():
        shutil.copytree(OLD_PUBLIC, PUBLIC)
    (PUBLIC / MAIN.lstrip('/')).write_bytes((BACK / 'main.after.js').read_bytes())
    (PUBLIC / 'index.html').write_bytes(expected_html)
    # 保留全部旧资产，保证未刷新的浏览器仍能加载原有分块脚本。
    unchanged = {}
    for path in OLD_PUBLIC.rglob('*'):
        if path.is_file() and path.relative_to(OLD_PUBLIC).as_posix() != 'index.html':
            relative = path.relative_to(OLD_PUBLIC)
            value = sha(path.read_bytes())
            assert sha((PUBLIC / relative).read_bytes()) == value, str(relative)
            unchanged['/' + relative.as_posix()] = value
    write(BACK / 'assets.preserved.json', json.dumps(unchanged))
    print(json.dumps({'prepared': True, 'database_bytes': db.stat().st_size, 'unchanged_assets': len(unchanged)}), flush=True)
elif MODE == 'candidate':
    assert CONFIG.read_bytes() == (BACK / 'Caddyfile.before').read_bytes()
    release = json.loads((BACK / 'release.json').read_text())
    image = release['image']
    assert image.startswith('ghcr.io/zzlye/new-api-custom-build@sha256:')
    assert release['commit'].startswith('baa390d9a')
    subprocess.run(['docker', 'pull', image], check=True)
    image_id = json.loads(run(['docker', 'image', 'inspect', image]))[0]['Id']
    names = run(['docker', 'ps', '-a', '--format', '{{.Names}}']).splitlines()
    assert CANDIDATE not in names and PREVIOUS not in names
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', PORT))
    cfg = json.loads((BACK / 'compose.before.json').read_text())
    service = copy.deepcopy(cfg['services'][OLD_SERVICE])
    service['image'] = image
    service['container_name'] = CANDIDATE
    service['ports'] = [f'127.0.0.1:{PORT}:3000']
    service['environment']['NODE_NAME'] = NODE
    cfg['services'][SERVICE] = service
    write(BACK / 'candidate.json', json.dumps(cfg, indent=2))
    subprocess.run(['docker', 'compose', '-p', 'new-api', '-f', str(BACK / 'candidate.json'), 'config', '--quiet'], check=True)
    subprocess.run(['docker', 'compose', '-p', 'new-api', '-f', str(BACK / 'candidate.json'), 'up', '-d', '--no-deps', SERVICE], check=True)
    for _ in range(90):
        candidate = inspect(CANDIDATE)
        if candidate['State'].get('Health', {}).get('Status') == 'healthy':
            break
        if not candidate['State']['Running']:
            raise RuntimeError('candidate stopped before verification')
        time.sleep(2)
    assert candidate['State'].get('Health', {}).get('Status') == 'healthy'
    assert candidate['Image'] == image_id
    health(f'http://127.0.0.1:{PORT}')
    auth(f'http://127.0.0.1:{PORT}')
    old = (BACK / 'Caddyfile.before').read_text()
    assert old.count('/data/newapi-preserved-ad8c4fc98') == 4
    assert old.count('reverse_proxy 127.0.0.1:3002') == 2
    final = old.replace('/data/newapi-preserved-ad8c4fc98', '/data/newapi-preserved-baa390d9a').replace('reverse_proxy 127.0.0.1:3002', f'reverse_proxy 127.0.0.1:{PORT}')
    # 验证入口只监听本机，复用实际入口完整路由，避免验证时绕过静态与鉴权规则。
    block = final[final.index('http://:3000 {'):]
    probe = block.replace('http://:3000 {', 'http://:3003 {\n    bind 127.0.0.1', 1)
    prepared = old + '\n' + probe
    write(BACK / 'Caddyfile.prepared', prepared)
    write(BACK / 'Caddyfile.after', final)
    reload_config(prepared)
    try:
        verify(PROBE)
        manifest = json.loads((BACK / 'assets.preserved.json').read_text())
        def check_asset(item):
            path, value = item
            assert sha(get(PROBE + path)) == value, ('preserved asset', path)
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(check_asset, manifest.items()))
        health('http://127.0.0.1:3000')
        assert sha(get('http://127.0.0.1:3000/')) == sha((BACK / 'index.before.html').read_bytes())
    except Exception:
        reload_config(old)
        raise
    write(BACK / 'ready.json', json.dumps({'image_id': image_id, 'image': image, 'assets_verified': len(manifest), 'checked_at': int(time.time())}))
    print('READY: candidate and frontend verified; public traffic unchanged', flush=True)
elif MODE == 'switch':
    assert (BACK / 'ready.json').exists()
    assert CONFIG.read_bytes() == (BACK / 'Caddyfile.prepared').read_bytes()
    verify(PROBE)
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
        write(BACK / 'switch-monitor.json', json.dumps(monitor))
    print('SWITCHED', json.dumps(monitor), flush=True)
elif MODE == 'finalize':
    assert (BACK / 'switched.json').exists()
    assert CONFIG.read_bytes() == (BACK / 'Caddyfile.after').read_bytes()
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
    current = inspect('new-api')
    assert current['Image'] == ready['image_id']
    network = current['NetworkSettings']['Networks']['new-api_new-api-network']
    resolved = run(['docker', 'exec', 'wenyun-workshop', 'node', '-e', "require('dns').lookup('new-api',(e,a)=>{if(e)process.exit(1);console.log(a)})"])
    extra_hosts = inspect('wenyun-workshop')['HostConfig'].get('ExtraHosts') or []
    gateway_route = 'new-api:' + network['Gateway'] in extra_hosts
    assert resolved == (network['Gateway'] if gateway_route else network['IPAddress']), 'workshop DNS mismatch'
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
    result = {'commit': 'baa390d9a', 'image': ready['image'], 'version': json.loads((BACK / 'version.json').read_text())['version'], 'frontend_main': MAIN, 'options_changed_since_backup': changes, 'previous_container': PREVIOUS, 'tasks': task_state(), 'completed_at': int(time.time())}
    write(BACK / 'VERIFICATION.json', json.dumps(result, ensure_ascii=False, indent=2))
    print(json.dumps(result, ensure_ascii=False), flush=True)
elif MODE == 'rollback':
    # 只回退入口和运行编排，不回滚数据库、不终止新旧容器中已接受的任务。
    health('http://127.0.0.1:3002')
    reload_config((BACK / 'Caddyfile.before').read_text())
    names = run(['docker', 'ps', '-a', '--format', '{{.Names}}']).splitlines()
    if PREVIOUS in names:
        if 'new-api' in names:
            run(['docker', 'rename', 'new-api', CANDIDATE])
        run(['docker', 'rename', PREVIOUS, 'new-api'])
    write(ROOT / 'compose.json', (BACK / 'compose.before.json').read_text())
    print('ROLLED BACK; all in-flight task containers retained', flush=True)
else:
    raise ValueError(MODE)
