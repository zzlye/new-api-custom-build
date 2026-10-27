import copy,hashlib,json,pathlib,re,socket,subprocess,threading,time,urllib.request,urllib.error
ROOT=pathlib.Path('/opt/new-api')
BACK=pathlib.Path('/mnt/data200/new-api/backups/media-keys-cd5638509-20261027')
IMAGE='ghcr.io/zzlye/new-api-custom-build@sha256:21e863ba3a61092b375a545a180e06bc44bc9f41f1c7457938762d8d16bd35e8'
OLD='new-api-next-ffec8e619'
SERVICE='new-api-next-cd5638509'
CANDIDATE='new-api-candidate-cd5638509'
PREVIOUS='new-api-previous-ffec8e619'
PORT=3007
CONFIG=ROOT/'caddy/Caddyfile'
MODE=globals().get('MODE','prepare')

def run(args):return subprocess.check_output(args,text=True).strip()
def inspect(name):return json.loads(run(['docker','inspect',name]))[0]
def sql(q):return run(['docker','exec','new-api-postgres','psql','-X','-v','ON_ERROR_STOP=1','-U','root','-d','new-api','-Atc',q])
def write(path,value):path.write_text(value,encoding='utf-8');path.chmod(0o600)
def get(base,path='/api/status'):
 with urllib.request.urlopen(base+path,timeout=20) as r:return r.read()
def health(base):
 j=json.loads(get(base));assert j['success'] and j['data']['version']==json.loads((BACK/'baseline.json').read_text())['version']
def connections(name):
 c=inspect(name)
 if not c['State']['Running']:return 0
 return len(run(['nsenter','-t',str(c['State']['Pid']),'-n','ss','-Hnt','state','established','sport','=',':3000']).splitlines())
def pending(node):return int(sql("select count(*) from async_relay_tasks where node_id='"+node+"' and status not in ('succeeded','failed','cancelled')"))
def reload_config(content):
 previous=CONFIG.read_text();CONFIG.write_text(content)
 try:
  for action in ['validate','reload']:subprocess.run(['docker','exec','new-api-caddy','caddy',action,'--config','/etc/caddy/Caddyfile','--adapter','caddyfile'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
 except Exception:
  CONFIG.write_text(previous);subprocess.run(['docker','exec','new-api-caddy','caddy','reload','--config','/etc/caddy/Caddyfile','--adapter','caddyfile'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE);raise

def verify(base):
 health(base)
 candidate=get('http://127.0.0.1:'+str(PORT),'/')
 for path in ['/','/media-keys','/keys','/register','/wallet']:
  assert get(base,path)==candidate,('frontend mismatch',base,path)
 for asset in re.findall(r'(?:src|href)="([^" ]+\.(?:js|css))"',candidate.decode()):
  if asset.startswith('/'):
   assert hashlib.sha256(get(base,asset)).digest()==hashlib.sha256(get('http://127.0.0.1:'+str(PORT),asset)).digest(),('asset mismatch',asset)
 for path in ['/api/user/self','/api/token/media-groups','/api/option/']:
  try:
   response=json.loads(get(base,path));assert response.get('success') is False or response.get('error'),('auth bypass',path)
  except urllib.error.HTTPError as e:assert e.code in (401,403)

if MODE=='prepare':
 BACK.mkdir(parents=True,mode=0o700,exist_ok=False)
 write(BACK/'compose.before.json',(ROOT/'compose.json').read_text());write(BACK/'Caddyfile.before',CONFIG.read_text())
 cs=json.loads(run(['docker','inspect']+run(['docker','ps','-aq']).splitlines()))
 write(BACK/'containers.before.json',json.dumps(cs))
 j=json.loads(get('http://127.0.0.1:3000'));write(BACK/'baseline.json',json.dumps({'version':j['data']['version'],'user_count':int(sql('select count(*) from users where deleted_at is null'))}))
 write(BACK/'options.before.json',sql("select coalesce(json_object_agg(key,md5(value)),'{}'::json) from options"))
 partial=BACK/'database.partial'
 with partial.open('xb') as f:
  partial.chmod(0o600);subprocess.run(['docker','exec','new-api-postgres','pg_dump','-U','root','-d','new-api','-Fc'],check=True,stdout=f)
 with partial.open('rb') as f:subprocess.run(['docker','exec','-i','new-api-postgres','pg_restore','--list'],check=True,stdin=f,stdout=subprocess.DEVNULL)
 partial.rename(BACK/'database.before.dump')
 print('BACKUP_OK',(BACK/'database.before.dump').stat().st_size,flush=True)
 subprocess.run(['docker','pull',IMAGE],check=True)
 print('IMAGE_READY',flush=True)
elif MODE=='candidate':
 assert (BACK/'database.before.dump').exists()
 assert CONFIG.read_text()==(BACK/'Caddyfile.before').read_text()
 cfg=json.loads((BACK/'compose.before.json').read_text())
 assert cfg['services'][OLD]['container_name']=='new-api'
 with socket.socket() as s:s.bind(('127.0.0.1',PORT))
 service=copy.deepcopy(cfg['services'][OLD]);service.update(image=IMAGE,container_name=CANDIDATE,ports=[f'127.0.0.1:{PORT}:3000'])
 service['environment']['NODE_NAME']='new-api-node-20261027-cd5638509'
 cfg['services'][SERVICE]=service
 write(BACK/'candidate.json',json.dumps(cfg,indent=2))
 subprocess.run(['docker','compose','-p','new-api','-f',str(BACK/'candidate.json'),'up','-d','--no-deps',SERVICE],check=True)
 for n in range(90):
  c=inspect(CANDIDATE);assert c['State']['Running']
  if c['State'].get('Health',{}).get('Status')=='healthy':break
  time.sleep(2)
 else:raise RuntimeError('候选容器健康检查超时')
 verify(f'http://127.0.0.1:{PORT}')
 # 页面统一由新镜像返回；旧静态资源保留兼容已打开的旧页面，找不到时转给新版。
 old=(BACK/'Caddyfile.before').read_text()
 new,count=re.subn(r'    @preserved_pages \{.*?\n    \}\n    handle @preserved_pages \{.*?\n    \}\n','',old,flags=re.S)
 assert count==2
 new=new.replace('path /static/* /favicon.ico','path /static/*')
 block='''    handle @preserved_assets {
        root * /data/newapi-preserved-baa390d9a
        header Cache-Control "no-cache, must-revalidate"
        file_server
    }'''
 replacement='''    handle @preserved_assets {
        root * /data/newapi-preserved-baa390d9a
        header Cache-Control "no-cache, must-revalidate"
        route {
            file_server {
                pass_thru
            }
            reverse_proxy 127.0.0.1:3007
        }
    }'''
 assert new.count(block)==2
 new=new.replace(block,replacement).replace('reverse_proxy 127.0.0.1:3006','reverse_proxy 127.0.0.1:3007')
 write(BACK/'Caddyfile.after',new)
 # 检查每位现有用户都已补齐默认密钥，且不会因重复启动生成多把。
 missing=int(sql('select count(*) from users u where u.deleted_at is null and not exists (select 1 from tokens t where t.default_for_user=u.id)'))
 duplicates=int(sql('select count(*) from (select default_for_user from tokens where default_for_user is not null group by default_for_user having count(*)>1) x'))
 assert missing==0 and duplicates==0,(missing,duplicates)
 print('CANDIDATE_OK',json.dumps({'missing_default_keys':missing,'duplicate_default_keys':duplicates}),flush=True)
elif MODE=='switch':
 assert CONFIG.read_text()==(BACK/'Caddyfile.before').read_text();health(f'http://127.0.0.1:{PORT}')
 stop=threading.Event();samples=[]
 def monitor():
  while not stop.is_set():
   try:health('http://127.0.0.1:3000');samples.append(True)
   except Exception:samples.append(False)
   stop.wait(.5)
 t=threading.Thread(target=monitor);t.start()
 try:
  reload_config((BACK/'Caddyfile.after').read_text())
  for b in ['http://127.0.0.1:3000','https://api.zzlye.xyz','https://api.zzlye.site','https://api.xn--v5z30ywuk.com']:verify(b)
  assert all(samples)
 except Exception:
  reload_config((BACK/'Caddyfile.before').read_text());raise
 finally:stop.set();t.join();write(BACK/'monitor.json',json.dumps({'samples':len(samples),'failures':samples.count(False)}))
 run(['docker','rename','new-api',PREVIOUS])
 try:run(['docker','rename',CANDIDATE,'new-api'])
 except Exception:run(['docker','rename',PREVIOUS,'new-api']);reload_config((BACK/'Caddyfile.before').read_text());raise
 cfg=json.loads((BACK/'candidate.json').read_text());cfg['services'].pop(OLD);cfg['services'][SERVICE]['container_name']='new-api'
 for s in cfg['services'].values():
  deps=s.get('depends_on')
  if isinstance(deps,list):s['depends_on']=[SERVICE if v==OLD else v for v in deps]
  elif isinstance(deps,dict) and OLD in deps:deps[SERVICE]=deps.pop(OLD)
 write(BACK/'compose.after.json',json.dumps(cfg,indent=2))
 run(['docker','compose','-p','new-api','-f',str(BACK/'compose.after.json'),'config','--quiet'])
 write(ROOT/'compose.json',(BACK/'compose.after.json').read_text())
 write(BACK/'switched.json',json.dumps({'at':int(time.time()),'image':IMAGE}))
 print('SWITCHED',json.dumps({'samples':len(samples),'failures':samples.count(False)}),flush=True)
elif MODE in ('status','consolidate'):
 assert (BACK/'switched.json').exists();health('http://127.0.0.1:3000')
 results=[]
 for c in json.loads((BACK/'containers.before.json').read_text()):
  name=c['Name'].lstrip('/')
  if name=='new-api':name=PREVIOUS
  if not name.startswith('new-api-previous-'):continue
  current=inspect(name)
  env=dict(x.split('=',1) for x in current['Config']['Env'] if '=' in x)
  node=env['NODE_NAME'];active=pending(node);conn=connections(name)
  entry={'name':name,'running':current['State']['Running'],'connections':conn,'unfinished_tasks':active}
  # 仅清理已经排空的历史服务；保留停用容器和镜像以便回退。
  if MODE=='consolidate' and current['State']['Running'] and active==0 and conn==0:
   # 排空后等待批量余额写回周期，再次确认没有任务或请求。
   time.sleep(7)
   if pending(node) or connections(name):
    results.append(entry);continue
   subprocess.run(['docker','update','--restart=no',name],check=True,stdout=subprocess.DEVNULL)
   subprocess.run(['docker','stop','--time','150',name],check=True,stdout=subprocess.DEVNULL)
   entry['running']=inspect(name)['State']['Running']
  results.append(entry)
 write(BACK/'drain.json',json.dumps(results,ensure_ascii=False,indent=2))
 print(json.dumps(results,ensure_ascii=False),flush=True)
elif MODE=='finalize':
 verify('http://127.0.0.1:3000');verify('https://api.zzlye.xyz')
 drain=json.loads((BACK/'drain.json').read_text())
 assert all(not inspect(e['name'])['State']['Running'] for e in drain)
 before=json.loads((BACK/'options.before.json').read_text())
 after=json.loads(sql("select coalesce(json_object_agg(key,md5(value)),'{}'::json) from options"))
 result={'commit':'cd5638509182ea945c4722188f89bb3784b9c99b','image':IMAGE,'version':json.loads((BACK/'baseline.json').read_text())['version'],'github_run':40440901798,'backup':str(BACK),'active_container':'new-api','port':PORT,'old_containers_stopped':[e['name'] for e in drain],'frontend_updated':True,'health_monitor':json.loads((BACK/'monitor.json').read_text()),'options_changed_since_backup':[k for k in set(before)|set(after) if before.get(k)!=after.get(k)],'missing_default_keys':int(sql('select count(*) from users u where u.deleted_at is null and not exists(select 1 from tokens t where t.default_for_user=u.id)')),'completed_at':int(time.time())}
 assert result['missing_default_keys']==0
 write(BACK/'release.json',json.dumps(result,ensure_ascii=False,indent=2))
 print(json.dumps(result,ensure_ascii=False),flush=True)
elif MODE=='rollback':
 # 回退只恢复旧容器与入口，不覆盖更新后产生的真实交易和日志。
 previous=inspect(PREVIOUS)
 if not previous['State']['Running']:subprocess.run(['docker','start',PREVIOUS],check=True)
 for _ in range(60):
  try:health('http://127.0.0.1:3006');break
  except Exception:time.sleep(1)
 else:raise RuntimeError('旧实例未恢复健康，保持当前入口')
 reload_config((BACK/'Caddyfile.before').read_text())
 run(['docker','rename','new-api',CANDIDATE]);run(['docker','rename',PREVIOUS,'new-api'])
 run(['docker','update','--restart=unless-stopped','new-api'])
 write(ROOT/'compose.json',(BACK/'compose.before.json').read_text())
 print('ROLLED_BACK; 新实例保留用于排空已接收请求',flush=True)
else:raise ValueError(MODE)


