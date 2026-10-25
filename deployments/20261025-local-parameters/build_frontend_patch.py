import hashlib
import json
import pathlib
import re
import sys

# 输入为本次线上备份，输出单独存放；不覆盖保留的原始前端文件。
backup, output = map(pathlib.Path, sys.argv[1:3])
root = pathlib.Path(__file__).resolve().parents[2]
source = (backup / 'main.before.js').read_bytes()
assert hashlib.sha256(source).hexdigest() == '2172558e545caaed923266d91d85feaeedf29e9e33ea6e1cfa8875877ef0ce62'
patched = source.decode('utf-8')


def replace_once(old, new):
    global patched
    assert patched.count(old) == 1, old[:100]
    patched = patched.replace(old, new)


message = 'Enter one parameter path or path=value per line (up to 1000 lines, 64 KB).'
validator = r'''function(value){if(new TextEncoder().encode(value).length>65536)return false;const lines=value.split("\n").map(line=>line.trim()).filter(Boolean);if(lines.length>1000)return false;return lines.every(line=>{const path=line.split("=",1)[0].trim();return path.length<=256&&/^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*$/.test(path)})}'''
replace_once('blocked_keywords:M.Yj().default(""),', 'blocked_keywords:M.Yj().default(""),local_parameter_rules:M.Yj().default("").refine(' + validator + ',{message:' + json.dumps(message) + '}),')
replace_once('e.blocked_keywords.trim()||', 'e.blocked_keywords.trim()||e.local_parameter_rules.trim()||')
# 复用线上表单控件和校验组件，只加入一个参数框，不改变既有页面结构。
field = '(0,a.jsx)(f.zB,{control:k.control,name:"local_parameter_rules",render:e=>{let{field:n}=e;return(0,a.jsxs)(f.eI,{children:[(0,a.jsx)(f.lR,{children:t("Local parameter rules")}),(0,a.jsx)(f.MJ,{children:(0,a.jsx)(_.T,{rows:3,placeholder:"generationConfig.thinkingConfig.thinkingLevel=MINIMAL",value:n.value??"",onChange:n.onChange})}),(0,a.jsx)(f.C5,{})]})}}),'
anchor = '(0,a.jsx)(y.p,{"aria-label":t("Search groups"),placeholder:t("Search groups"),value:i,onChange:e=>b(e.target.value)})'
replace_once(anchor, field + anchor)
locales = ['en', 'fr', 'ja', 'ru', 'vi', 'zh-TW', 'zh']
matches = list(re.finditer(r'"Blocked request keywords":("(?:\\.|[^"\\])*")', patched))
assert len(matches) == len(locales)
for language, match in reversed(list(zip(locales, matches))):
    locale = json.loads((root / 'web/src/i18n/locales' / f'{language}.json').read_text(encoding='utf8'))['translation']
    assert json.loads(match.group(1).replace("\\'", "'")) == locale['Blocked request keywords'], language
    addition = ',' + json.dumps('Local parameter rules') + ':' + json.dumps(locale['Local parameter rules'], ensure_ascii=False) + ',' + json.dumps(message) + ':' + json.dumps(locale[message], ensure_ascii=False)
    patched = patched[:match.end()] + addition + patched[match.end():]
data = patched.encode('utf-8')
digest = hashlib.sha256(data).hexdigest()
assert digest == '477101abcccf85f8279d74813c9821d5300f8db572d2182c91389874af53d890'
old_path = json.loads((backup / 'assets.json').read_text())['main']
new_path = '/static/js/index.parameters-' + digest[:12] + '.js'
html = (backup / 'index.before.html').read_bytes()
assert html.count(old_path.encode()) == 1
html = html.replace(old_path.encode(), new_path.encode())
output.mkdir(parents=True, exist_ok=True)
(output / 'main.after.js').write_bytes(data)
(output / 'index.after.html').write_bytes(html)
(output / 'assets.after.json').write_text(json.dumps({'main': new_path, 'sha256': digest}), encoding='utf-8')
print(json.dumps({'main': new_path, 'sha256': digest}))
