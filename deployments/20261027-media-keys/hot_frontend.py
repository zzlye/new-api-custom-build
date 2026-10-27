import pathlib,re,json,subprocess,tarfile,urllib.request,urllib.error,hashlib,threading,time
root=pathlib.Path('/opt/new-api')
config=root/'caddy/Caddyfile'
backup=pathlib.Path('/mnt/data200/new-api/backups/fixed-group-8ef31039a-20261027')
dest=root/'caddy/data/newapi-fixed-group-8ef31039a'
archive=pathlib.Path('/mnt/data200/new-api/backups/newapi-fixed-group-8ef31039a.tar.gz')
assert hashlib.sha256(archive.read_bytes()).hexdigest()=='1f3422b6d6690c7377781a5800892cf12779736bc6558a1e32eac6691f1c478e'
backup.mkdir(mode=0o700,exist_ok=False)
before=config.read_text();(backup/'Caddyfile.before').write_text(before);(backup/'Caddyfile.before').chmod(0o600)
dest.mkdir(mode=0o755,exist_ok=False)
with tarfile.open(archive) as t:
 # 仅解压此次构建的静态文件，拒绝链接和越界路径。
 for m in t.getmembers():
  p=(dest/m.name).resolve()
  assert p.is_relative_to(dest.resolve()) and m.isfile(),m.name
 t.extractall(dest,filter='data')
html=(dest/'index.html').read_bytes()
bundles=list((dest/'static/js').rglob('*.js'))
assert any('Fixed group' in p.read_text() for p in bundles)
assert all('Managed by root' not in p.read_text() for p in bundles)
# 页面列表沿用现有发布规则，补齐新增的独立密钥页面。
original=(pathlib.Path('/mnt/data200/new-api/backups/media-keys-cd5638509-20261027')/'Caddyfile.before').read_text()
pages=re.findall(r'    @preserved_pages \{.*?\n    \}\n    handle @preserved_pages \{.*?\n    \}\n',original,re.S)
assert len(pages)==2 and pages[0]==pages[1]
page=pages[0].replace('preserved_pages','fixed_group_pages').replace('/data/newapi-preserved-baa390d9a','/data/newapi-fixed-group-8ef31039a').replace('path / /index.html','path /media-keys /media-keys/ / /index.html')
assert before.count('    @preserved_assets {')==2
new=before.replace('    @preserved_assets {',page+'    @preserved_assets {')
old='''    handle @preserved_assets {
        root * /data/newapi-preserved-baa390d9a
        header Cache-Control "no-cache, must-revalidate"
        route {
            file_server {
                pass_thru
            }
            reverse_proxy 127.0.0.1:3007
        }
    }'''
replacement='''    handle @preserved_assets {
        header Cache-Control "no-cache, must-revalidate"
        route {
            root * /data/newapi-fixed-group-8ef31039a
            file_server {
                pass_thru
            }
            root * /data/newapi-preserved-baa390d9a
            file_server {
                pass_thru
            }
            reverse_proxy 127.0.0.1:3007
        }
    }'''
assert new.count(old)==2
new=new.replace(old,replacement)
def caddy(action):
 result=subprocess.run(['docker','exec','new-api-caddy','caddy',action,'--config','/etc/caddy/Caddyfile','--adapter','caddyfile'],capture_output=True,text=True)
 if result.returncode:raise RuntimeError(result.stderr)
def get(base,path):
 with urllib.request.urlopen(base+path,timeout=20) as r:return r.read()
stop=threading.Event();samples=[]
def monitor():
 while not stop.is_set():
  try:samples.append(bool(json.loads(get('http://127.0.0.1:3000','/api/status'))['success']))
  except Exception:samples.append(False)
  stop.wait(.5)
thread=threading.Thread(target=monitor);thread.start()
try:
 config.write_text(new);caddy('validate');caddy('reload')
 bases=['http://127.0.0.1:3000','https://api.zzlye.xyz','https://api.zzlye.site','https://api.xn--v5z30ywuk.com']
 for base in bases:
  status=json.loads(get(base,'/api/status'));assert status['success'] and status['data']['version']==''
  for path in ['/','/media-keys','/keys','/register','/wallet']:
   assert get(base,path)==html,(base,path)
  for path in re.findall(r'(?:src|href)="([^" ]+\.(?:js|css))"',html.decode()):
   if path.startswith('/'):
    assert hashlib.sha256(get(base,path)).digest()==hashlib.sha256((dest/path.lstrip('/')).read_bytes()).digest(),path
  for path in ['/api/user/self','/api/token/media-groups','/api/option/']:
   try:
    r=json.loads(get(base,path));assert r.get('success') is False or r.get('error')
   except urllib.error.HTTPError as e:assert e.code in (401,403)
 # 核对实际承载分组文案的异步资源，不仅检查首页文件。
 translated=[p for p in bundles if 'Fixed group' in p.read_text()]
 for p in translated:
  assert get(bases[1],'/'+p.relative_to(dest).as_posix())==p.read_bytes()
 assert samples and all(samples)
except Exception:
 config.write_text(before);caddy('reload');raise
finally:stop.set();thread.join()
(backup/'Caddyfile.after').write_text(new);(backup/'Caddyfile.after').chmod(0o600)
result={'commit':'8ef31039a','frontend_hot_updated':True,'version_unchanged':True,'entrypoints_verified':len(bases),'old_assets_preserved':True,'health_samples':len(samples),'health_failures':samples.count(False),'label':'固定','completed_at':int(time.time())}
(backup/'release.json').write_text(json.dumps(result,ensure_ascii=False,indent=2));print(json.dumps(result,ensure_ascii=False))
