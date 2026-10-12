#!/usr/bin/env python3
"""Compare raw Go benchmark logs; every path needs >=5 samples."""
import pathlib, re, statistics, json
root=pathlib.Path(__file__).resolve().parent
pattern=re.compile(r'^(BenchmarkPerformance\S+)-\d+\s+\d+\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
def parse(path):
 result={}
 for line in path.read_text().splitlines():
  m=pattern.match(line)
  if m: result.setdefault(m[1],[]).append(tuple(map(float,m.groups()[1:])))
 return result
base=parse(root/'baseline-bench.log'); candidate=parse(root/'candidate-bench.log')
paired=root/'paired-results.json'
overrides=json.loads(paired.read_text()) if paired.exists() else {}
rows=[]; failures=[]
for name in sorted(base.keys() | candidate.keys()):
 a=base.get(name,[]); b=candidate.get(name,[])
 if len(a)<5 or len(b)<5: failures.append(name+': fewer than five samples');continue
 am=[statistics.median(v[i] for v in a) for i in range(3)]
 bm=[statistics.median(v[i] for v in b) for i in range(3)]
 ratio=bm[0]/am[0]
 decision_ratio=ratio
 if name in overrides:
  repeat=overrides[name]
  decision_ratio=statistics.median(v[0] for v in repeat['candidate'])/statistics.median(v[0] for v in repeat['baseline'])
 if name in overrides:
  repeat=overrides[name]
  pa=repeat['baseline'];pb=repeat['candidate']
  if len(pa)<5 or len(pb)<5: failures.append(name+': incomplete paired repeat')
  for i in (1,2):
   if statistics.median(v[i] for v in pb)>statistics.median(v[i] for v in pa): failures.append(name+': paired allocation regression')
 if decision_ratio>1.1: failures.append(name+': CPU regression >10%')
 if bm[1]>am[1] or bm[2]>am[2]:failures.append(name+': allocation regression')
 if '/complete/' in name or '/partial/' in name or '/string/' in name or '/disabled/' in name:
  if bm[1:]!=[0,0]:failures.append(name+': expected zero allocation')
 if name.startswith('BenchmarkPerformanceLabels/direct/'):
  limit=0 if name.endswith('/0') else 1
  if bm[2]>limit: failures.append(name+': conversion budget')
 if name.startswith('BenchmarkPerformanceRecording/') and '/disabled/' not in name and name.rsplit('/',1)[-1] in ('64','256'):
  if bm[1]>=am[1] or bm[2]>=am[2]:failures.append(name+': missing recording reduction')
 rows.append([name,len(a),len(b),*am,*bm,ratio,decision_ratio])
selection=root/'paired-selection.json'
if selection.exists():
 for name in json.loads(selection.read_text())['selected']:
  if name not in overrides:failures.append(name+': paired repeat not completed')
expected={f'BenchmarkPerformanceLabels/{m}/{n}' for m in ('direct','copy_control') for n in (0,8,64,256)}
expected|={f'BenchmarkPerformanceRecording/{m}/{k}/{n}' for m in ('noop','memory','disabled') for k in ('counter','histogram','gauge') for n in (0,8,64,256)}
expected|={f'BenchmarkPerformanceScope/{m}/{n}' for m in ('complete','partial','string') for n in (0,8,64,256)}
if set(base)!=expected or set(candidate)!=expected:failures.append('missing or unexpected benchmark paths; expected 56')
if not rows:failures.append('no measurements')
(root/'comparison.json').write_text(json.dumps({'rows':rows,'failures':failures},indent=2))
lines=['# Baseline / candidate benchmark comparison','','Five samples each, same Go/OS/arch; median values. CPU gate uses paired repeat when recorded. Copy-control and direct conversion are unchanged controls; only full recording and scope exercise implementation differences.','','| Path | Base ns | Candidate ns | Ratio | Base B / allocs | Candidate B / allocs | Gate CPU ratio |','|---|---:|---:|---:|---:|---:|---:|']
for name,na,nb,an,ab,aa,bn,bb,ba,ratio,dr in rows:
 lines.append(f'| {name} | {an:g} | {bn:g} | {ratio:.3f} | {ab:g} / {aa:g} | {bb:g} / {ba:g} | {dr:.3f} |')
lines+=['','Failures: '+('; '.join(failures) if failures else 'none')]
(root/'comparison.md').write_text('\n'.join(lines)+'\n')
print('\n'.join(failures) if failures else f'PASS: {len(rows)} benchmark paths')
