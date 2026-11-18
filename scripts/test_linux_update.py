import pathlib,tempfile,subprocess,os,json
root=pathlib.Path(__file__).resolve().parents[1];script=(root/'deployments/linux/update.sh').read_text();results=[]
# Simulate administrator-owned transaction paths in a temporary tree only.
script=script.replace('${EUID} -ne 0 || ', '')
for mode,active in [('success',True),('success',False),('stage',True),('backup',True),('stop',True),('replace',True),('start',True)]:
 with tempfile.TemporaryDirectory(prefix='uwa-update-') as tmp:
  d=pathlib.Path(tmp);bin=d/'bin';bin.mkdir();tools=d/'tools';tools.mkdir();(bin/'uwa-gateway').write_text('OLD');(d/'new').write_text('NEW');state=d/'state';state.write_text('active' if active else 'inactive')
  env=os.environ.copy();env.update({'PATH':str(tools)+':'+env['PATH'],'UWA_FAULT':mode,'UWA_FAKE_BIN':str(bin),'UWA_STATE':str(state)})
  for cmd in ['systemctl','install','cp','mv']:
   p=tools/cmd;p.write_text('''#!/usr/bin/env python3
import os,sys,pathlib
cmd=pathlib.Path(sys.argv[0]).name;a=sys.argv[1:];mode=os.environ['UWA_FAULT'];state=pathlib.Path(os.environ['UWA_STATE']);target=pathlib.Path(os.environ['UWA_FAKE_BIN'])/'uwa-gateway'
if cmd=='systemctl':
 if a[0]=='is-active':sys.exit(0 if state.read_text()=='active' else 3)
 if a[0]=='stop':
  state.write_text('inactive')
  sys.exit(1 if mode=='stop' else 0)
 if a[0]=='start':
  if mode=='start' and target.read_text()=='NEW':sys.exit(1)
  state.write_text('active');sys.exit(0)
 sys.exit(2)
if cmd=='install' and mode=='stage':sys.exit(1)
if cmd=='cp' and mode=='backup' and a[-1].endswith('.previous'):sys.exit(1)
if cmd=='mv' and mode=='replace' and '.uwa-update.' in a[-2]:sys.exit(1)
os.execv('/usr/bin/'+cmd,[cmd]+a)
''');p.chmod(0o755)
  runner=d/'update.sh';runner.write_text(script.replace('/usr/local/bin',str(bin)));p=subprocess.run(['bash',str(runner),str(d/'new')],env=env,capture_output=True,text=True,timeout=10)
  result={'mode':mode,'initial_active':active,'exit':p.returncode,'final_binary':(bin/'uwa-gateway').read_text(),'final_state':state.read_text(),'stderr':p.stderr}
  # Failed stop can also reject rollback's stop; restart must still restore state.
  assert (p.returncode==0)==(mode=='success'),result
  assert result['final_binary']==('NEW' if mode=='success' else 'OLD'),result
  assert result['final_state']==('active' if active else 'inactive'),result
  results.append(result)
print(json.dumps(results,indent=2));print('PASS',len(results),'isolated shell transaction scenarios; not native systemd certification')
