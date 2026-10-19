// Run each real WASM demo in its own process. No tile service is needed:
// unavailable tiles exercise the asynchronous error path without network I/O.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { setTimeout as delay } from 'node:timers/promises';
const demo = process.argv[2];
assert.ok(['explorer', 'mapview', 'mapview3d'].includes(demo));
// Go disables Fetch when process.argv0 identifies Node. Give the WASM
// runtime a browser-like process descriptor so the stub owns every request.
const nodeProcess = process;
globalThis.process = Object.create(nodeProcess);
Object.defineProperty(globalThis.process, 'argv0', { value: 'browser-smoke' });
globalThis.fetch = async () => { throw new Error('Offline smoke test'); };
vm.runInThisContext(await readFile('web/wasm_exec.js', 'utf8'));
const go = new Go();
if (demo === 'explorer') go.argv = ['explorer', '-renderer', 'software', '-offline'];
let exitCode;
go.exit = code => { exitCode = code; };
const { instance } = await WebAssembly.instantiate(await readFile(`web/demos/${demo}.wasm`), go.importObject);
const timeout = setTimeout(() => { console.error(`${demo} timed out`); process.exit(1); }, 20000);
const running = go.run(instance);
async function expectOutput(text) {
  let output = '';
  for (let n = 0; n < 200 && !output.includes(text); n++) {
    await delay(25);
    output += bubbletea_read();
  }
  assert.ok(output.includes(text), `expected output: ${text}; got ${output.slice(-1000)}`);
  return output;
}
try {
  for (let n = 0; typeof globalThis.bubbletea_read !== 'function' && n < 100; n++) await delay(20);
  assert.equal(typeof globalThis.bubbletea_read, 'function');
  bubbletea_resize(100, 40);
  const initial = await expectOutput(demo === 'explorer' ? '2D' : demo === 'mapview' ? 'Loading' : 'map zoom');
  if (demo === 'explorer') {
    assert.ok(initial.includes('\x1b[?1003h'), 'explorer enables terminal mouse motion reporting');
    bubbletea_write('\x1b[<64;90;10M'); // wheel over the wide-layout 2D map
    await delay(60);
  }
  bubbletea_write('\x1b]7501;?\x1b\\');
  const advertised = await expectOutput(':msg=');
  const encoded = advertised.match(/:msg=([A-Za-z0-9+/=]+)/)?.[1];
  assert.ok(encoded, 'action status payload');
  const description = Buffer.from(encoded, 'base64').toString('utf8');
  assert.ok(description.includes(demo === 'explorer' ? '2D / 3D' : demo === 'mapview' ? 'North' : 'Rotate left'));
  bubbletea_resize(60, 20);
  if (demo === 'explorer') {
    bubbletea_write('v');
    await expectOutput('heights:'); // bundled city rendered through the real app
    await delay(80);
    bubbletea_read();
    // Browser terminals deliver SGR click, held-button motion, release and wheel.
    // Native integration tests additionally assert camera/center changes and
    // pane-offset isolation for both wide and narrow layouts.
    bubbletea_write('\x1b[<0;16;7M\x1b[<32;22;9M\x1b[<0;22;9m');
    await delay(120);
    assert.ok(bubbletea_read().length > 0, '3D mouse drag produces a redraw');
    bubbletea_write('\x1b[<64;16;7M');
    bubbletea_write('w');
    await delay(50);
    bubbletea_write('v');
    bubbletea_resize(61, 21);
    // Native transition tests assert the restored geographic center. A terminal
    // diff need not repeat the unchanged 'D' in the status label here.
    await delay(100);
    bubbletea_write('\x1b[<0;16;7M\x1b[<32;20;9M\x1b[<0;1;1m'); // 2D grab/release outside
    await delay(100);
    bubbletea_read();
    bubbletea_write('v');
    await expectOutput('heights:'); // same neighborhood, preserved camera
    bubbletea_write('R');
    await expectOutput('Could not load'); // explicit refresh with live loading disabled
  } else {
    bubbletea_write(demo === 'mapview' ? '\x1b[C' : 'n');
  }
  await delay(100);
  bubbletea_write('\x03');
  await running;
  assert.equal(exitCode, 0);
  console.log(`${demo}: startup, actions, resize, input, and quit passed`);
} finally {
  clearTimeout(timeout);
}
process.exit(0);
