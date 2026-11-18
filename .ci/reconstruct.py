#!/usr/bin/env python3
"""Reconstruct hash-pinned source; never import or execute candidate code."""
import argparse,base64,hashlib,json,os,pathlib,re,subprocess,tempfile,urllib.request
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs): return None
p=argparse.ArgumentParser();p.add_argument('manifest');p.add_argument('sha256');p.add_argument('destination');a=p.parse_args()
raw=pathlib.Path(a.manifest).read_bytes()
if hashlib.sha256(raw).hexdigest()!=a.sha256:raise RuntimeError('manifest SHA256 mismatch')
m=json.loads(raw);dst=pathlib.Path(a.destination)
if dst.exists():raise RuntimeError('destination exists')
sha_re=re.compile(r'^[0-9a-f]{40}$')
refs=sorted({m['base']}|{v['head'] for v in m.get('pr_sources',[])})
if any(not sha_re.fullmatch(x) for x in refs):raise RuntimeError('invalid commit identity')
token=os.environ.pop('GH_TOKEN','');opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
cache={};planned=[];seen=set()
with tempfile.TemporaryDirectory() as td:
 root=pathlib.Path(td);repo=root/'objects.git';home=root/'home';home.mkdir()
 env={'PATH':os.defpath,'HOME':str(home),'LANG':'C.UTF-8','GIT_CONFIG_NOSYSTEM':'1','GIT_CONFIG_GLOBAL':os.devnull,'GIT_TERMINAL_PROMPT':'0'}
 def git(*args):return subprocess.run(['git','--git-dir='+str(repo),*args],env=env,capture_output=True,timeout=600)
 if git('init','--bare').returncode:raise RuntimeError('git init failed')
 # Public source only; authentication is not passed to git or persisted.
 for i in range(0,len(refs),20):
  r=git('-c','http.followRedirects=false','fetch','--no-tags','--depth=1','https://github.com/chenhg5/cc-connect.git',*refs[i:i+20])
  print('public commit prefetch batch',i//20,'status',r.returncode,flush=True)
 def blob(sha):
  if not sha_re.fullmatch(sha):raise RuntimeError('invalid blob identity')
  if sha in cache:return cache[sha]
  r=git('cat-file','blob',sha)
  if r.returncode==0:b=r.stdout
  else:
   headers={'Accept':'application/vnd.github+json','User-Agent':'cc-connect-source-reconstruction'}
   if token:headers['Authorization']='Bearer '+token
   req=urllib.request.Request('https://api.github.com/repos/chenhg5/cc-connect/git/blobs/'+sha,headers=headers)
   with opener.open(req,timeout=60) as response:
    data=response.read(48*1024*1024+1)
   if len(data)>48*1024*1024:raise RuntimeError('blob response too large')
   obj=json.loads(data)
   if obj.get('encoding')!='base64':raise RuntimeError('unexpected blob encoding')
   b=base64.b64decode(obj['content'])
  if hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()!=sha:raise RuntimeError('Git blob identity mismatch')
  cache[sha]=b;return b
 for f in m['files']:
  name=f['path'];path=pathlib.PurePosixPath(name)
  if path.is_absolute() or str(path)!=name or any(x in ('.git','..') for x in path.parts) or '\\' in name or not path.parts or name in seen:raise RuntimeError('unsafe source path')
  if f['mode'] not in ('100644','100755'):raise RuntimeError('unsupported mode')
  seen.add(name);chunks=[]
  for op in f['ops']:
   if set(op)=={'text'}:chunks.append(op['text'].encode())
   elif set(op)=={'copy'}:
    sha,start,end=op['copy'];b=blob(sha)
    if not isinstance(start,int) or not isinstance(end,int) or not 0<=start<=end<=len(b):raise RuntimeError('invalid slice')
    chunks.append(b[start:end])
   else:raise RuntimeError('invalid operation')
  b=b''.join(chunks)
  if len(b)!=f['size'] or hashlib.sha256(b).hexdigest()!=f['sha256'] or hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()!=f['git_blob']:raise RuntimeError('source digest mismatch')
  planned.append((path,b,int(f['mode'][-3:],8)))
 dst.mkdir()
 for path,b,mode in planned:
  out=dst/path;out.parent.mkdir(parents=True,exist_ok=True);out.write_bytes(b);out.chmod(mode)
 print('verified source files:',len(planned),'manifest SHA256:',a.sha256)
