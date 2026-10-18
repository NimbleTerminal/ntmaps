import os, subprocess
from pathlib import Path
base=Path('/tmp/osm-perf-round2')
env=dict(os.environ,GOWORK=str(base/'local.work'),NTCHARTS3D_TRANSPORT_GPU_TEST='0')
with (base/'results/validation.txt').open('w') as log:
 for module in ['nt3d','ntgpuimage','ntcharts3d','ntcharts-osm','ntcharts-osm/examples']:
  for args in [['test','-race','./...'],['vet','./...']]:
   cmd=['go','-C',str(base/module),*args]
   print(' '.join(cmd),flush=True);log.write('\n$ '+' '.join(cmd)+'\n');log.flush()
   subprocess.run(cmd,env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
 for variant in ['baseline','local']:
  env.update(GOWORK=str(base/(variant+'.work')),NT3D_PIXEL_HASHES=str(base/f'results/pixels-{variant}.txt'))
  cmd=['go','-C',str(base/'ntcharts-osm/examples'),'test','-run','^TestConsumerPixelOracle$','-count=1','./mapview3d']
  print('pixel oracle:',variant,flush=True)
  subprocess.run(cmd,env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
 assert (base/'results/pixels-baseline.txt').read_bytes()==(base/'results/pixels-local.txt').read_bytes(),'pixel hashes differ'
 print('All 216 rendered-pixel hashes match',flush=True)
 log.write('\nAll 216 rendered-pixel hashes match.\n')
