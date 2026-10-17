"""Exercise an explicitly selected local Docker deployment with disposable FASTQ files."""
import argparse,gzip,json,secrets,urllib.request,urllib.error,urllib.parse,uuid
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--env',required=True);p.add_argument('--host-dir',required=True);args=p.parse_args()
env=dict(line.split('=',1) for line in Path(args.env).read_text().splitlines() if '=' in line and not line.startswith('#'))
base='http://127.0.0.1:8080/api/v1';checks=0

def call(method,path,token=None,body=None,status=200):
 global checks
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 request=urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,headers=headers,method=method)
 try:
  with urllib.request.urlopen(request,timeout=60) as response:
   code=response.status;raw=response.read();result=json.loads(raw) if raw else {}
 except urllib.error.HTTPError as response:code=response.code;result=json.load(response)
 assert code==status,(method,path,code,result.get('error',''))
 checks+=1;return result.get('data',result)

def login(email,password):return call('POST','/auth/login',body={'email':email,'password':password})['access_token']
admin=login('admin@schemabio.local',env['DEFAULT_ADMIN_PASSWORD'].strip('"'))
config=call('GET','/data/config',admin);container_root=config['local_import_directory'].rstrip('/')
assert container_root.startswith('/')
host_root=Path(args.host_dir).resolve();assert host_root.is_dir()
tag='local-import-check-'+secrets.token_hex(6);folder=host_root/tag;folder.mkdir()
identity='verify-'+tag;created=[];user=None
payload=lambda first,second:{'internal_id':identity,'read1_path':container_root+'/'+tag+'/'+first,'read2_path':container_root+'/'+tag+'/'+second}
raw1=gzip.compress(b'@fixture/1\nACGT\n+\nIIII\n');raw2=gzip.compress(b'@fixture/2\nACGT\n+\nIIII\n')
fixtures={'R1.fastq.gz':raw1,'R2.fastq.gz':raw2,'bad_R1.fastq.gz':raw1,'bad_R2.fastq.gz':raw2[:-5]}
try:
 for name,data in fixtures.items():(folder/name).write_bytes(data)
 password='A8!'+secrets.token_urlsafe(20);email=tag+'@example.invalid'
 user=call('POST','/users',admin,{'email':email,'name':'Temporary local import test','password':password,'system_role':'USER','permissions':['data.manage']},201)
 ordinary=login(email,password);uid=str(user['id'])
 body=payload('R1.fastq.gz','R2.fastq.gz')
 denied=call('POST','/data/local-import',ordinary,body,403);assert denied['required_permission']=='samples.write'
 call('PUT','/users/'+uid,admin,{'permissions':['data.manage','samples.write']})
 assets=call('POST','/data/local-import',ordinary,body,201);created.extend(asset['id'] for asset in assets)
 assert len(assets)==2 and len(set(created))==2
 for asset in assets:
  uuid.UUID(asset['id']);assert asset['source']=='local_import' and asset['validation_status']=='valid'
  assert asset['internal_id']==identity
  expected=raw1 if asset['read_type']=='read1' else raw2
  assert asset['file_size']==len(expected)
  request=urllib.request.Request(base+'/data/assets/'+asset['id']+'/download',headers={'Authorization':'Bearer '+admin})
  with urllib.request.urlopen(request) as response:assert response.read()==expected
 call('POST','/data/local-import',ordinary,body,409)
 bad=call('POST','/data/local-import',ordinary,payload('bad_R1.fastq.gz','bad_R2.fastq.gz'),400);assert bad['code']=='local_import_integrity'
 same=call('POST','/data/local-import',ordinary,payload('R1.fastq.gz','R1.fastq.gz'),400);assert same['code']=='local_import_same_file'
 call('POST','/data/local-import',ordinary,{**body,'read1_path':'/etc/passwd'},400)
 listing=call('GET','/data/assets?search='+urllib.parse.quote(identity),admin)
 assert listing['total']==2 and set(asset['id'] for asset in listing['items'])==set(created)
 call('PUT','/users/'+uid,admin,{'permissions':[]})
 call('POST','/data/local-import',ordinary,body,403)
 print('PASS:',checks,'local checks: mounted paths, complete FASTQ pair, sizes, UUIDs, downloads, corrupt files, duplicates, permissions and revocation')
finally:
 try:
  for asset_id in created:call('DELETE','/data/assets/'+asset_id,admin,status=204)
 finally:
  try:
   if user:
    record=call('GET','/users/'+str(user['id']),admin);assert record['email']==email
    call('DELETE','/users/'+str(user['id']),admin,status=204)
  finally:
   for name in fixtures:
    target=folder/name
    if target.exists():target.unlink()
   folder.rmdir()
 print('Disposable accounts, file registrations and FASTQ fixtures cleaned')
