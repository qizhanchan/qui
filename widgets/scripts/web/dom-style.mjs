// Records getComputedStyle for each fixture under htmlcss/testdata/style/
// in real Chrome, writing <name>.golden.json next to the fixture.
// htmlcss/dom_style_test.go computes the same page's styles through htmlcss
// and diffs them per element and property, so the goldens are committed and
// `go test` never needs Chrome.
//
// A fixture is one file, <name>.html; every element with an id is recorded
// for the PROPS below (resolved values, as getComputedStyle returns them).
//
// Regenerate (needs Chrome and `npm ci` in widgets/scripts/web, see
// chrome.mjs):
//   REFRESH_REF=1 go test ./htmlcss -run DOMStyle
// which runs: node widgets/scripts/web/dom-style.mjs [fixture-name ...]

import {launchChrome} from './chrome.mjs';
import {fileURLToPath, pathToFileURL} from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const fixtureDir = path.resolve(__dirname, '../../../htmlcss/testdata/style');
const VIEWPORT = {width: 640, height: 480, deviceScaleFactor: 1};

// Must stay in step with the serializers in htmlcss/dom_style_test.go.
const PROPS = [
  'display', 'visibility', 'position', 'opacity',
  'color', 'background-color',
  'font-size', 'font-weight', 'font-style', 'line-height',
  'text-align', 'text-transform', 'letter-spacing', 'text-decoration-line', 'white-space',
  'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
  'border-top-color', 'border-top-left-radius', 'box-sizing',
  'flex-grow', 'flex-shrink',
];

function measure(props) {
  const out = {};
  for (const el of document.querySelectorAll('[id]')) {
    const cs = getComputedStyle(el);
    const rec = {};
    for (const p of props) rec[p] = cs.getPropertyValue(p);
    out[el.id] = rec;
  }
  return out;
}

async function main() {
  let names = process.argv.slice(2);
  if (names.length === 0) {
    names = (await fs.readdir(fixtureDir))
      .filter((f) => f.endsWith('.html'))
      .map((f) => f.slice(0, -'.html'.length));
  }
  const browser = await launchChrome();
  try {
    for (const name of names.sort()) {
      const page = await browser.newPage();
      page.on('pageerror', (e) => console.log(`  [pageerror] ${e.message}`));
      await page.setViewport(VIEWPORT);
      await page.goto(pathToFileURL(path.join(fixtureDir, `${name}.html`)).toString(), {waitUntil: 'load'});
      const styles = await page.evaluate(measure, PROPS);
      await page.close();
      const out = path.join(fixtureDir, `${name}.golden.json`);
      // One line per element keeps golden diffs readable.
      const lines = Object.entries(styles).map(([id, s]) => `  ${JSON.stringify(id)}: ${JSON.stringify(s)}`);
      await fs.writeFile(out, `{
 "fixture": ${JSON.stringify(name)},
 "chrome": ${JSON.stringify(await browser.version())},
 "styles": {
${lines.join(',\n')}
 }
}
`);
      console.log(`wrote ${out} (${Object.keys(styles).length} elements)`);
    }
  } finally {
    await browser.close();
  }
}

await main();
