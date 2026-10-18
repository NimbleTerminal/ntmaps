import os,subprocess
from pathlib import Path
base=Path('/tmp/osm-perf-round2');repo=base/'ntcharts-osm'
env=dict(os.environ,GOWORK=str(base/'local-wasm.work'),GOOS='js',GOARCH='wasm',CGO_ENABLED='0')
(repo/'web/demos').mkdir(parents=True,exist_ok=True)
with (base/'results/browser.txt').open('w') as log:
 for demo in ['mapview','mapview3d']:
  subprocess.run(['go','-C',str(repo/'examples'),'build','-o',str(repo/f'web/demos/{demo}.wasm'),'./'+demo],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
 native=dict(os.environ,GOWORK=str(base/'local.work'))
 subprocess.run(['go','-C',str(repo/'examples'),'tool','booba-assets','--no-index','../web/'],env=native,stdout=log,stderr=subprocess.STDOUT,check=True)
 for demo in ['mapview','mapview3d']:
  subprocess.run(['node','scripts/smoke-demo.mjs',demo],cwd=repo,stdout=log,stderr=subprocess.STDOUT,check=True)
print('Both browser builds and lifecycle tests passed')
