import concurrent.futures
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import threading
import time
import urllib.error
import urllib.request

# 此脚本仅用于已完成镜像构建的本次热切换，所有检查通过前不转移线上流量。
MODE = globals().get('MODE', 'prepare')
ROOT = pathlib.Path('/opt/new-api')
BACK = ROOT / 'backups/gemini-stream-ad8c4fc98-20261025'
ASSETS = ROOT / 'backups/gemini-stream-assets-v2-20261025'
PUBLIC = ROOT / 'caddy/data/newapi-preserved-ad8c4fc98'
CONFIG = ROOT / 'caddy/Caddyfile'
CANDIDATE = 'new-api-candidate-ad8c4fc98'
OLDNODE = 'new-api-node-20261023-48deefc61'
NEWNODE = 'new-api-node-20261025-ad8c4fc98'
PREVIOUS = 'new-api-previous-48deefc61'
NEW_SERVICE = 'new-api-next-ad8c4fc98'
OLD_SERVICE = 'new-api-next-48deefc61'
IMAGE = 'ghcr.io/zzlye/new-api-custom-build@sha256:e8a6a41d099649345e7ce69307b76baee0a081b4bbc477e4e796460ba4e66392'
PROBE = 'http://127.0.0.1:3003'
UI_PATHS = '/ /index.html /privacy-policy /user-agreement /forgot-password /oauth /oauth/* /otp /register /reset /sign-in /sign-up /401 /403 /404 /500 /503 /chat2link /about /pricing /pricing/* /rankings /setup /user/reset /channels /chat/* /dashboard /dashboard/* /errors/* /keys /lottery /models /models/* /order-logs /playground /profile /redemption-codes /security /subscriptions /system-info /system-settings /system-settings/* /task-plugins /usage-logs /usage-logs/* /users /wallet'

# 同时保留页面尾部斜杠写法，避免直达页面意外载入新版静态资源。
UI_PATHS += ' ' + ' '.join(path + '/' for path in UI_PATHS.split() if path != '/' and '*' not in path and path != '/index.html')

def run(args):
    return subprocess.check_output(args, text=True).strip()


def sql(query):
    return run(['docker', 'exec', 'new-api-postgres', 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', 'root', '-d', 'new-api', '-Atc', query])


def write(path, content):
    path.write_text(content, encoding='utf-8')
    path.chmod(0o600)


def get(url):
    with urllib.request.urlopen(url, timeout=25) as response:
        return response.read()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inspect(name):
    return json.loads(run(['docker', 'inspect', name]))[0]


def health(base):
    data = json.loads(get(base + '/api/status'))
    assert data['success'] and data['data']['version'] == '', ('health/version', base)


def auth(base):
    for path in ['/api/user/self', '/api/channel/', '/dashboard/billing/subscription']:
        try:
            response = urllib.request.urlopen(base + path, timeout=15)
        except urllib.error.HTTPError as error:
            assert error.code in (401, 403), (path, error.code)
        else:
            data = json.loads(response.read())
            assert data.get('success') is False or data.get('error'), ('authentication bypass', path)


def reload_config(content):
    previous = CONFIG.read_text()
    CONFIG.write_text(content)
    try:
        for action in ['validate', 'reload']:
            subprocess.run(['docker', 'exec', 'new-api-caddy', 'caddy', action, '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    except Exception:
        CONFIG.write_text(previous)
        subprocess.run(['docker', 'exec', 'new-api-caddy', 'caddy', 'reload', '--config', '/etc/caddy/Caddyfile', '--adapter', 'caddyfile'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        raise


def static_block():
    # 只固定公开的页面与静态资源；API、媒体签名验证和支付回调仍由新版后端处理。
    return '''    @preserved_assets {
        method GET HEAD
        path /static/* /favicon.ico
    }
    handle @preserved_assets {
        root * /data/newapi-preserved-ad8c4fc98
        header Cache-Control "no-cache, must-revalidate"
        file_server
    }
    @preserved_pages {
        method GET HEAD
        path ''' + UI_PATHS + '''
        not path /dashboard/billing/*
    }
    handle @preserved_pages {
        root * /data/newapi-preserved-ad8c4fc98
        rewrite * /index.html
        header Cache-Control "no-cache, must-revalidate"
        file_server
    }
'''


def verify(base):
    health(base)
    auth(base)
    expected = json.loads((BACK / 'assets.before.json').read_text())
    for path, value in expected.items():
        assert digest(get(base + path)) == value, ('frontend changed', base, path)
    for path in ['/register', '/wallet', '/system-settings/request-policies/local-request-validation', '/lottery']:
        assert digest(get(base + path)) == expected['/'], ('page changed', path)
    # 媒体直链不得变成前端页面，缺失参数时必须继续由后端拒绝。
    try:
        response = urllib.request.urlopen(base + '/task-media/nonexistent/audio/0', timeout=15)
    except urllib.error.HTTPError as error:
        assert error.code in (400, 401, 403, 404)
    else:
        assert 'text/html' not in response.headers.get('Content-Type', ''), 'media intercepted'


def task_state():
    return sql("select node_id,status,count(*) from async_relay_tasks where status not in ('succeeded','failed','cancelled') group by node_id,status")


if MODE == 'prepare':
    assert CONFIG.read_text() == (BACK / 'Caddyfile.before').read_text(), 'live configuration changed'
    candidate = inspect(CANDIDATE)
    assert candidate['Image'] == IMAGE.split('@')[1]
    assert candidate['State']['Health']['Status'] == 'healthy'
    health('http://127.0.0.1:3002')
    manifest = json.loads((ASSETS / 'manifest.json').read_text())
    for name, value in manifest.items():
        path = (ASSETS / 'dist' / name).resolve()
        assert path.is_relative_to((ASSETS / 'dist').resolve())
        assert digest(path.read_bytes()) == value, name
    if PUBLIC.exists():
        raise RuntimeError('preserved asset directory already exists; inspect before retry')
    shutil.copytree(ASSETS / 'dist', PUBLIC)
    # 原站存在独立修复过的入口脚本，必须保留实际对外提供的版本，不能只复制镜像内的旧脚本。
    expected = json.loads((BACK / 'assets.before.json').read_text())
    for path, value in expected.items():
        body = get('http://127.0.0.1:3000' + path)
        assert digest(body) == value, path
        target = PUBLIC / ('index.html' if path == '/' else path.lstrip('/'))
        target.write_bytes(body)
        manifest['index.html' if path == '/' else path.lstrip('/')] = value
    write(BACK / 'frontend.preserved.json', json.dumps(manifest))
    old = CONFIG.read_text()
    # 本地验证入口仅监听环回地址，不暴露额外的公网管理端口。
    probe = '\nhttp://:3003 {\n    bind 127.0.0.1\n' + static_block() + '    handle {\n        reverse_proxy 127.0.0.1:3002\n    }\n}\n'
    prepared = old + probe
    write(BACK / 'Caddyfile.prepared', prepared)
    reload_config(prepared)
    try:
        verify(PROBE)
        def check_asset(item):
            name, value = item
            assert digest(get(PROBE + '/' + name)) == value, name
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(check_asset, manifest.items()))
        verify('http://127.0.0.1:3000')
    except Exception:
        reload_config(old)
        raise
    # 线上仍指向旧后端；候选路径已逐文件核对，之后才允许切换。
    start = old.index('api.zzlye.xyz, api.zzlye.site, api.xn--v5z30ywuk.com {')
    end = old.index('\nak.zzlye.site {', start)
    public_site = 'api.zzlye.xyz, api.zzlye.site, api.xn--v5z30ywuk.com {\n' + static_block() + '    handle {\n        reverse_proxy 127.0.0.1:3002\n    }\n}\n'
    final = old[:start] + public_site + old[end:]
    start = final.index('http://:3000 {')
    assert final[start:].count('127.0.0.1:3001') == 1
    final = final[:start] + 'http://:3000 {\n' + static_block() + '''    handle {
        reverse_proxy 127.0.0.1:3002 {
            trusted_proxies 172.19.0.3/32
        }
    }
}
'''
    assert '127.0.0.1:3001' not in final
    write(BACK / 'Caddyfile.preserved.after', final)
    write(BACK / 'preserved-ready.json', json.dumps({'assets_checked': len(manifest), 'image': candidate['Image'], 'checked_at': int(time.time())}))
    print('READY', len(manifest), 'assets match; candidate authentication/health/version OK; live unchanged', flush=True)
elif MODE == 'switch':
    assert (BACK / 'preserved-ready.json').exists()
    assert CONFIG.read_text() == (BACK / 'Caddyfile.prepared').read_text()
    verify(PROBE)
    # 切换期间持续采样入口，不重启数据库、缓存或旧业务容器。
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
    started = int(time.time())
    try:
        reload_config((BACK / 'Caddyfile.preserved.after').read_text())
        for base in ['http://127.0.0.1:3000', 'https://api.zzlye.xyz', 'https://api.zzlye.site', 'https://api.xn--v5z30ywuk.com']:
            verify(base)
        write(BACK / 'switched.json', json.dumps({'time': started, 'image': IMAGE, 'frontend_preserved': True}))
    except Exception:
        reload_config((BACK / 'Caddyfile.prepared').read_text())
        raise
    finally:
        stop.set()
        thread.join()
        write(BACK / 'switch-monitor.json', json.dumps(monitor))
    print('SWITCHED', json.dumps(monitor), 'old_tasks', task_state(), flush=True)
elif MODE == 'finalize':
    assert (BACK / 'switched.json').exists()
    assert CONFIG.read_text() == (BACK / 'Caddyfile.preserved.after').read_text()
    # 先迁移内部服务名，旧节点继续完成已有异步任务，不强行终止。
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
    assert current['Image'] == IMAGE.split('@')[1]
    network = current['NetworkSettings']['Networks']['new-api_new-api-network']
    resolved = run(['docker', 'exec', 'wenyun-workshop', 'node', '-e', "require('dns').lookup('new-api',(e,a)=>{if(e)process.exit(1);console.log(a)})"])
    # 工坊已通过固定网关访问 Caddy，不能误把正常网关解析当作容器解析失败。
    extra_hosts = inspect('wenyun-workshop')['HostConfig'].get('ExtraHosts') or []
    gateway_route = 'new-api:' + network['Gateway'] in extra_hosts
    expected_address = network['Gateway'] if gateway_route else network['IPAddress']
    assert resolved == expected_address, ('internal DNS', resolved, expected_address)
    if gateway_route:
        assert '127.0.0.1:3001' not in CONFIG.read_text(), 'internal gateway still uses old backend'
    run(['docker', 'exec', 'wenyun-workshop', 'node', '-e', "fetch('http://new-api:3000/api/status').then(r=>r.json()).then(j=>{if(!j.success||j.data.version!=='')process.exit(1);console.log('internal OK')}).catch(()=>process.exit(1))"])
    cfg = json.loads((BACK / 'compose.before.json').read_text())
    new = json.loads((BACK / 'candidate.json').read_text())['services'][NEW_SERVICE]
    new['image'] = IMAGE
    new['container_name'] = 'new-api'
    cfg['services'].pop(OLD_SERVICE)
    cfg['services'][NEW_SERVICE] = new
    for service in cfg['services'].values():
        dependencies = service.get('depends_on')
        if isinstance(dependencies, list):
            service['depends_on'] = [NEW_SERVICE if name == OLD_SERVICE else name for name in dependencies]
        elif isinstance(dependencies, dict) and OLD_SERVICE in dependencies:
            dependencies[NEW_SERVICE] = dependencies.pop(OLD_SERVICE)
    write(ROOT / 'compose.json', json.dumps(cfg, indent=2))
    run(['docker', 'compose', '-p', 'new-api', '-f', str(ROOT / 'compose.json'), 'config', '--quiet'])
    verify('http://127.0.0.1:3000')
    verify('https://api.zzlye.xyz')
    before = json.loads((BACK / 'options.before.json').read_text())
    after = json.loads(sql("select coalesce(json_object_agg(key,md5(value)),'{}'::json) from options"))
    changes = [key for key in set(before) | set(after) if before.get(key) != after.get(key)]
    for saved in json.loads((BACK / 'containers.before.json').read_text())[1:]:
        assert inspect(saved['Name'])['State']['StartedAt'] == saved['State']['StartedAt'], saved['Name']
    result = {'commit': 'ad8c4fc98', 'image': IMAGE, 'version': '', 'frontend_preserved': True, 'options_changed_since_backup': changes, 'previous_container': PREVIOUS, 'old_tasks': task_state(), 'completed_at': int(time.time())}
    write(BACK / 'VERIFICATION.json', json.dumps(result, ensure_ascii=False, indent=2))
    print(json.dumps(result, ensure_ascii=False), flush=True)
elif MODE == 'status':
    print(task_state())
    print(run(['docker', 'ps', '--format', '{{.Names}} {{.Status}} {{.Ports}}']))
elif MODE == 'rollback':
    names = run(['docker', 'ps', '-a', '--format', '{{.Names}}']).splitlines()
    if PREVIOUS in names:
        run(['docker', 'start', PREVIOUS])
        health('http://127.0.0.1:3001')
        if 'new-api' in names:
            run(['docker', 'rename', 'new-api', CANDIDATE])
        run(['docker', 'rename', PREVIOUS, 'new-api'])
    health('http://127.0.0.1:3001')
    reload_config((BACK / 'Caddyfile.before').read_text())
    write(ROOT / 'compose.json', (BACK / 'compose.before.json').read_text())
    print('ROLLED BACK; candidate retained for in-flight tasks', flush=True)
else:
    raise ValueError(MODE)