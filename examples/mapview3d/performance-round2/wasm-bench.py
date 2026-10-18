import os,subprocess
from pathlib import Path
base=Path('/tmp/osm-perf-round2')
text=(base/'presentation-baseline.work').read_text()+'\nreplace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2 v2.0.0-20261009201241-c397c32a5074\nreplace github.com/atotto/clipboard => github.com/neomantra/clipboard v0.0.0-20260506203444-114abb315f8d\n'
(base/'presentation-baseline-wasm.work').write_text(text)
goroot=subprocess.check_output(['go','env','GOROOT'],text=True).strip()
for variant in ['baseline','local']:
 env=dict(os.environ,GOWORK=str(base/('presentation-baseline-wasm.work' if variant=='baseline' else 'local-wasm.work')),GOOS='js',GOARCH='wasm',CGO_ENABLED='0')
 print('WASM presentation:',variant,flush=True)
 with (base/f'results/wasm-glyph-{variant}.txt').open('w') as log:
  subprocess.run(['go','-C',str(base/'ntcharts3d'),'test','-exec='+goroot+'/lib/wasm/go_js_wasm_exec','-run','^TestPresentationPump$','-bench','^BenchmarkPresentation/glyph/(camera|texture)$','-benchtime=20x','-count=3','.'],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
