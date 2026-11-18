// Records each Canvas 2D fixture under htmlcss/canvas2d/testdata/ in real
// Chrome, writing <name>.png next to the <name>.json op list.
// htmlcss/canvas2d/chrome_test.go replays the same ops through canvas2d
// and compares pixels against these goldens, so they are committed and
// `go test` never needs Chrome.
//
// A fixture is {"w", "h", "ops": [[name, ...args], ...]}: a property
// assignment when the name is a context attribute, else a method call.
// "$x=method" stores the result as $x; "$x.method" calls on it; a "$x"
// argument passes it. ":checker" names the shared 8×8 test image (see
// checker()); "copyImageData" is getImageData + putImageData. Text uses
// GoRegular, loaded from go-regular.ttf; the Go test registers the same face.
//
// Regenerate (needs Chrome and `npm ci` here, see chrome.mjs):
//   node widgets/scripts/web/canvas-2d.mjs [fixture-name ...]

import {launchChrome} from './chrome.mjs';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const fixtureDir = path.resolve(__dirname, '../../../htmlcss/canvas2d/testdata');
// Inlined: a setContent page may not load file:// fonts.
const font = 'data:font/ttf;base64,' + (await fs.readFile(path.join(__dirname, 'go-regular.ttf'))).toString('base64');

// Runs in the page. Self-contained: it is serialized.
async function play(fx) {
  await document.fonts.load('16px GoRegular');
  const cv = document.createElement('canvas');
  cv.width = fx.w; cv.height = fx.h;
  document.body.appendChild(cv);
  const ctx = cv.getContext('2d');
  const checker = () => {
    const c = document.createElement('canvas');
    c.width = 8; c.height = 8;
    const d = new ImageData(8, 8);
    for (let y = 0; y < 8; y++) for (let x = 0; x < 8; x++) {
      const o = 4 * (y * 8 + x);
      const on = ((x >> 2) + (y >> 2)) % 2 === 0;
      d.data.set(on ? [40, 160, 40, 255] : [255, 255, 255, 160], o);
    }
    c.getContext('2d').putImageData(d, 0, 0);
    return {canvas: c, data: d};
  };
  const vars = {};
  const arg = (a) => (typeof a === 'string' && a.startsWith('$') ? vars[a] : a);
  for (const [name, ...raw] of fx.ops) {
    const args = raw.map(arg);
    let m;
    if ((m = /^\$(\w+)=(\w+)(?::checker)?$/.exec(name))) {
      vars['$' + m[1]] = name.endsWith(':checker') ? ctx[m[2]](checker().canvas, ...args) : ctx[m[2]](...args);
    } else if ((m = /^\$(\w+)\.(\w+)$/.exec(name))) {
      vars['$' + m[1]][m[2]](...args);
    } else if (name === 'putImageData:checker') {
      ctx.putImageData(checker().data, ...args);
    } else if (name === 'drawImage:checker') {
      ctx.drawImage(checker().canvas, ...args);
    } else if (name === 'copyImageData') {
      const [sx, sy, w, h, dx, dy] = args;
      ctx.putImageData(ctx.getImageData(sx, sy, w, h), dx, dy);
    } else if (typeof ctx[name] === 'function') {
      ctx[name](...args);
    } else {
      ctx[name] = args[0];
    }
  }
  return cv.toDataURL('image/png');
}

async function main() {
  let names = process.argv.slice(2);
  if (names.length === 0) {
    names = (await fs.readdir(fixtureDir)).filter((f) => f.endsWith('.json')).map((f) => f.slice(0, -5));
  }
  const browser = await launchChrome(['--font-render-hinting=none', '--disable-gpu', '--disable-accelerated-2d-canvas']);
  try {
    for (const name of names.sort()) {
      const fx = JSON.parse(await fs.readFile(path.join(fixtureDir, `${name}.json`), 'utf8'));
      const page = await browser.newPage();
      page.on('pageerror', (e) => console.log(`  [pageerror] ${e.message}`));
      await page.setContent(`<!doctype html><style>@font-face{font-family:GoRegular;src:url(${font})}</style><body>`);
      const url = await page.evaluate(play, fx);
      await page.close();
      const out = path.join(fixtureDir, `${name}.png`);
      await fs.writeFile(out, Buffer.from(url.split(',')[1], 'base64'));
      console.log(`wrote ${out}`);
    }
  } finally {
    await browser.close();
  }
}

await main();
