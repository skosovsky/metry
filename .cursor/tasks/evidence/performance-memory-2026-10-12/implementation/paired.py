#!/usr/bin/env python3
"""Repeat noisy (>20% sample range) or >10% slower cases in alternating pairs."""
import os, pathlib, json, re, statistics, subprocess
folder=pathlib.Path(__file__).resolve().parent
root=folder.parents[4]
env=dict(os.environ,GOWORK='off',GOCACHE='/tmp/metry-performance-cache')
pat=re.compile(r'^(BenchmarkPerformance\S+)-\d+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
def samples(log):
 d={}
 for line in log.splitlines():
  m=pat.match(line)
  if m:d.setdefault(m[1],[]).append([float(v) for v in m.groups()[1:]])
 return d
base=samples((folder/'baseline-bench.log').read_text()); cand=samples((folder/'candidate-bench.log').read_text())
selected=[]
for name,a in base.items():
 b=cand[name]
 av=[v[0] for v in a];bv=[v[0] for v in b]
 if max(av)/min(av)>1.2 or max(bv)/min(bv)>1.2 or statistics.median(bv)/statistics.median(av)>1.1:selected.append(name)
(folder/'paired-selection.json').write_text(json.dumps({'instability_rule':'max/min ns within either revision >1.2, or candidate/base median >1.1','selected':selected},indent=2))
if not selected:print('No repeat needed',flush=True);raise SystemExit()
# Exact hierarchical names; slash-separated regex is handled by Go component matching.
# Run individual names so unrelated sibling component combinations are not selected.
results={}; commands=[]
for name in selected:
 print('PAIR',name,flush=True)
 package='./genai' if name.startswith('BenchmarkPerformanceScope') else '.'
 pattern='/'.join('^'+re.escape(part)+'$' for part in name.split('/'))
 collected={'baseline':[],'candidate':[]}
 for pair in range(5):
  order=['baseline','candidate'] if pair%2==0 else ['candidate','baseline']
  for revision in order:
   cwd=pathlib.Path('/tmp/metry-performance-baseline') if revision=='baseline' else root
   args=['go','test',package,'-run','^$','-bench',pattern,'-benchmem','-benchtime=500ms','-count=1']
   p=subprocess.run(args,cwd=cwd,env=env,capture_output=True,text=True)
   logname=f'paired-{name.replace("/","_")}-{pair}-{revision}.log'
   (folder/logname).write_text(f'cwd: {cwd}\ncommand: {args!r}\n'+p.stdout+p.stderr+f'\nEXIT_CODE={p.returncode}\n')
   commands.append({'name':name,'pair':pair,'revision':revision,'args':args,'cwd':str(cwd),'exit':p.returncode,'log':logname})
   (folder/'paired-commands.json').write_text(json.dumps(commands,indent=2))
   if p.returncode:raise SystemExit(p.returncode)
   found=samples(p.stdout)
   if set(found)!={name}:raise RuntimeError(f'wrong benchmark selection {name}: {found.keys()}')
   collected[revision].append(found[name][0])
 ratios=[b[0]/a[0] for a,b in zip(collected['baseline'],collected['candidate'])]
 results[name]={'baseline':collected['baseline'],'candidate':collected['candidate'],'pair_ratios':ratios,'median_pair_ratio':statistics.median(ratios),'ratio':statistics.median(v[0] for v in collected['candidate'])/statistics.median(v[0] for v in collected['baseline'])}
 (folder/'paired-results.json').write_text(json.dumps(results,indent=2))
 print('RESULT',name,results[name]['ratio'],flush=True)
