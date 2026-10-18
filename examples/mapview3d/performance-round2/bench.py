import os, subprocess
from pathlib import Path
base=Path('/tmp/osm-perf-round2')
for variant in ['baseline','local']:
 env=dict(os.environ,GOWORK=str(base/(variant+'.work')))
 print('Snapshot matrix:',variant,flush=True)
 with (base/f'results/snapshot-{variant}.txt').open('w') as log:
  subprocess.run(['go','-C',str(base/'ntcharts-osm/examples'),'test','-run','^$','-bench','BenchmarkMap3D','-benchmem','-benchtime=144x','-count=3','./mapview3d'],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
for variant in ['baseline','local']:
 env=dict(os.environ,GOWORK=str(base/('presentation-baseline.work' if variant=='baseline' else 'local.work')))
 print('Presentation matrix:',variant,flush=True)
 with (base/f'results/presentation-{variant}.txt').open('w') as log:
  subprocess.run(['go','-C',str(base/'ntcharts3d'),'test','-run','^$','-bench','^BenchmarkPresentation/(glyph|direct)/','-benchmem','-benchtime=72x','-count=3','.'],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
  for workload in ['camera','texture','bars','burst']:
   for sample in range(3):
    subprocess.run(['go','-C',str(base/'ntcharts3d'),'test','-run','^$','-bench',f'^BenchmarkPresentation/shm/{workload}$','-benchmem','-benchtime=20x','-count=1','.'],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
print('All benchmarks complete',flush=True)
