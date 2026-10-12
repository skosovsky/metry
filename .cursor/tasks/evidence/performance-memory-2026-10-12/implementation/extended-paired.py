#!/usr/bin/env python3
"""Extend ALL failing paired median-time cases; retain every previous sample."""
import os,pathlib,json,statistics,subprocess,re
folder=pathlib.Path(__file__).resolve().parent;root=folder.parents[4]
env=dict(os.environ,GOWORK='off',GOCACHE='/tmp/metry-performance-cache')
pattern=re.compile(r'^(BenchmarkPerformance\S+)-\d+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
p=folder/'paired-results.json';results=json.loads(p.read_text())
# Preserve initial data and diagnostic pair ratios. Gate uses the ratio of medians.
initial=folder/'paired-initial-results.json'
if not initial.exists():initial.write_text(json.dumps(results,indent=2))
commands=[]
selected=[]
for name,v in results.items():
 v['median_pair_ratio']=statistics.median(v['pair_ratios'])
 v['ratio']=statistics.median(s[0] for s in v['candidate'])/statistics.median(s[0] for s in v['baseline'])
 if v['ratio']>1.1:selected.append(name)
p.write_text(json.dumps(results,indent=2))
(folder/'extended-selection.json').write_text(json.dumps({'rule':'All failing ratio-of-median-ns cases; append 10 pairs at1s; retain every initial sample in final gate','selected':selected},indent=2))
for name in selected:
 print('EXTEND',name,flush=True)
 package='./genai' if name.startswith('BenchmarkPerformanceScope') else '.'
 expression='/'.join('^'+re.escape(part)+'$' for part in name.split('/'))
 for pair in range(10):
  for revision in (['baseline','candidate'] if pair%2==0 else ['candidate','baseline']):
   cwd=pathlib.Path('/tmp/metry-performance-baseline') if revision=='baseline' else root
   args=['go','test',package,'-run','^$','-bench',expression,'-benchmem','-benchtime=1s','-count=1']
   proc=subprocess.run(args,cwd=cwd,env=env,capture_output=True,text=True)
   logname=f'extended-{name.replace("/","_")}-{pair}-{revision}.log'
   (folder/logname).write_text(f'cwd: {cwd}\ncommand: {args!r}\n'+proc.stdout+proc.stderr+f'\nEXIT_CODE={proc.returncode}\n')
   commands.append({'name':name,'pair':pair,'revision':revision,'cwd':str(cwd),'args':args,'exit':proc.returncode,'log':logname})
   (folder/'extended-commands.json').write_text(json.dumps(commands,indent=2))
   if proc.returncode:raise SystemExit(proc.returncode)
   found=[m for line in proc.stdout.splitlines() if (m:=pattern.match(line))]
   if len(found)!=1 or found[0][1]!=name:raise RuntimeError('wrong benchmark selection')
   results[name][revision].append([float(s) for s in found[0].groups()[1:]])
 v=results[name]
 v['pair_ratios']=[b[0]/a[0] for a,b in zip(v['baseline'],v['candidate'])]
 v['median_pair_ratio']=statistics.median(v['pair_ratios'])
 v['ratio']=statistics.median(s[0] for s in v['candidate'])/statistics.median(s[0] for s in v['baseline'])
 p.write_text(json.dumps(results,indent=2))
 print('RESULT',name,v['ratio'],flush=True)
