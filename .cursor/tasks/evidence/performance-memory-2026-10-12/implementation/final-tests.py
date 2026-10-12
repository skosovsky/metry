from pathlib import Path
import os,subprocess,json
root=Path(__file__).resolve().parents[5]
folder=root/'.cursor/tasks/evidence/performance-memory-2026-10-12/implementation'
env=dict(os.environ,GOWORK='off',GOCACHE='/tmp/metry-performance-cache')
commands=[]
for name,args in [('final-root-tests',['go','test','-count=1','./...']),('final-root-race',['go','test','-race','-count=1','./...']),('boundary-e2e',['go','test','-race','-count=1','-tags=e2e','-run','^(TestE2EGenAIScope|TestE2EMetricsRegistry|TestE2EPropagation)','-v','.'])]:
 print('START',name,flush=True)
 with (folder/(name+'.log')).open('w') as out:
  out.write(f'cwd: {root}\ncommand: {args!r}\n');out.flush()
  p=subprocess.run(args,cwd=root,env=env,stdout=out,stderr=subprocess.STDOUT)
  out.write(f'\nEXIT_CODE={p.returncode}\n')
 commands.append({'name':name,'args':args,'cwd':str(root),'exit':p.returncode})
 (folder/'final-commands.json').write_text(json.dumps(commands,indent=2))
 print('END',name,p.returncode,flush=True)
 if p.returncode:break
