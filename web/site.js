import { BoobaTerminal, installActionBar, installRendererHud, parseRendererFromURL } from './booba/booba.js';

const demos = { explorer: 'Map explorer', mapview: 'Simple 2D map', mapview3d: 'Flat map in 3D' };
const help = {
 explorer: 'Drag to pan/orbit · Shift-drag pans in 3D · scroll to zoom · click places · v switches 2D/3D · j/k choose a place · / filters · arrows pan/look · WASD moves in 3D · t GPU/software · R refreshes buildings · q quits.',
 mapview: 'Arrows pan · +/- zoom · g glyph/Kitty · q quit.',
 mapview3d: 'Arrows tilt/rotate · WASD pan · [] map zoom · +/- camera zoom · n/p place · r rotate · t renderer · g graphics · q quit.',
};
const requested = new URLSearchParams(location.search).get('demo');
const demo = Object.hasOwn(demos, requested) ? requested : 'explorer';
const status = document.querySelector('#status');
document.querySelector('#title').textContent = demos[demo];
document.title = `${demos[demo]} · ntmaps`;
const restart = new URL(location.href);
restart.searchParams.set('demo', demo);
document.querySelector('#restart').href = restart.href;
for (const link of document.querySelectorAll('[data-demo]')) {
  if (link.dataset.demo === demo) link.setAttribute('aria-current', 'page');
}
document.querySelector('#help').textContent = help[demo];
if (!window.isSecureContext) {
  // Safari 26+ runs a non-localhost http page in its Enhanced Security process
  // with the JIT disabled, which makes the WebAssembly about twenty times
  // slower, and WebGPU exists only in secure contexts. Say so instead of
  // leaving the visitor guessing why the demo crawls.
  const notice = document.createElement('p');
  notice.id = 'notice';
  notice.textContent = `This page is on an insecure origin (${location.origin}). Safari runs it without a JIT and without WebGPU, so it is far slower than it should be. Open it over https or from localhost.`;
  status.insertAdjacentElement('afterend', notice);
}
function failed(error) {
  status.dataset.error = '';
  status.textContent = `Could not run the demo: ${error.message}. Try Restart.`;
  console.error(error);
}
async function main() {
  const go = new Go();
  const response = await fetch(`demos/${demo}.wasm`);
  if (!response.ok) throw new Error(`Download failed (${response.status})`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  const running = go.run(instance);
  running.then(() => { status.textContent = 'Session ended. Choose Restart to run again.'; }, failed);
  const terminal = new BoobaTerminal('terminal', { renderer: parseRendererFromURL(), allowOSC7501Actions: true });
  await terminal.init();
  terminal.setActionKeyEncoder({
    canEncode: gesture => terminal.encodeKey(gesture) !== null,
    encode: gesture => terminal.encodeKey(gesture),
  });
  installActionBar(terminal.actions, {
    parent: document.querySelector('#controls'),
    openKeyboard: () => terminal.focus(),
    onLayout: () => terminal.fitAddon?.fit(),
  });
  terminal.onActionError = failed;
  window.addEventListener('pagehide', () => terminal.dispose(), { once: true });
  terminal.connectWasm();
  terminal.focus();
  installRendererHud(terminal.term, {
    parent: document.querySelector('.terminal-frame'),
    position: 'absolute',
    bindToggleHotkey: false,
  });
  status.textContent = 'Running locally in your browser. No terminal server required.';
}
main().catch(failed);
