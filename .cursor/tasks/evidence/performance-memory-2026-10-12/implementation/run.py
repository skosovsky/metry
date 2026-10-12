import subprocess, pathlib, json, platform, os, time
root=pathlib.Path(__file__).resolve().parents[5]
evidence=root/'.cursor/tasks/evidence/performance-memory-2026-10-12/implementation'
env=dict(os.environ,GOWORK='off',GOCACHE='/tmp/metry-performance-cache')
meta={'baseline':'215af3ee33bcc4b58f0b532da1b1958c3990bb8a','platform':platform.platform(),'go':subprocess.check_output(['go','version'],text=True).strip(),'environment':{'GOWORK':'off','GOCACHE':env['GOCACHE']},'commands':[]}
(evidence/'environment.json').write_text(json.dumps(meta,indent=2))
for name,cwd,args in [
 ('root-tests',root,['go','test','./...']),
 ('root-race',root,['go','test','-race','./...']),
 ('grpc-tests',root/'middleware/grpc',['go','test','./...']),
 ('grpc-race',root/'middleware/grpc',['go','test','-race','./...']),
 ('otlp-tests',root/'integration/otlp',['go','test','-tags=integration','./...']),
 ('otlp-race',root/'integration/otlp',['go','test','-race','-tags=integration','./...']),
 ('baseline-bench',pathlib.Path('/tmp/metry-performance-baseline'),['go','test','.','./genai','-run','^$','-bench','^BenchmarkPerformance','-benchmem','-benchtime=200ms','-count=5']),
 ('candidate-bench',root,['go','test','.','./genai','-run','^$','-bench','^BenchmarkPerformance','-benchmem','-benchtime=200ms','-count=5']),
]:
 print('START',name,flush=True)
 with (evidence/(name+'.log')).open('w') as out:
  out.write(f'cwd: {cwd}\ncommand: {args!r}\n');out.flush()
  result=subprocess.run(args,cwd=cwd,env=env,stdout=out,stderr=subprocess.STDOUT)
  out.write(f'\nEXIT_CODE={result.returncode}\n')
 meta['commands'].append({'name':name,'cwd':str(cwd),'args':args,'exit':result.returncode})
 (evidence/'environment.json').write_text(json.dumps(meta,indent=2))
 print('END',name,result.returncode,flush=True)
 if result.returncode: break
