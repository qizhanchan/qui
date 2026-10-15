// Records the layout geometry of each fixture under htmlcss/testdata/layout/
// in real Chrome, writing <name>.golden.json next to the fixture.
// htmlcss/dom_layout_test.go renders the same page through htmlcss and diffs
// every element's box against these goldens, so the goldens are committed
// and `go test` never needs Chrome.
//
// A fixture is one file, <name>.html: plain HTML + <style>, parsed as-is by
// htmlcss. Every element with an id is recorded as its border box from
// getBoundingClientRect, in page coordinates: [x, y, w, h], rounded to
// 1/100 px. Fixtures set font-family 'GoRegular', which they @font-face-load
// from widgets/scripts/web/go-regular.ttf; the Go test registers the same
// face under that name, so text-driven sizes come from identical metrics.
// An element's data-tol attribute widens the Go side's tolerance for it.
//
// Regenerate (needs Chrome + puppeteer-core from /opt/js_work/material-web,
// same setup as dom-events.mjs):
//   REFRESH_REF=1 go test ./htmlcss -run DOMLayout
// which runs: node widgets/scripts/web/dom-layout.mjs [fixture-name ...]

import puppeteer from '/opt/js_work/material-web/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js';
import {fileURLToPath, pathToFileURL} from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const fixtureDir = path.resolve(__dirname, '../../../htmlcss/testdata/layout');
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
// Must match domLayoutViewport in htmlcss/dom_layout_test.go.
const VIEWPORT = {width: 640, height: 480, deviceScaleFactor: 1};

// Runs in the page. Kept self-contained: it is serialized.
function measure() {
  const r2 = (v) => Math.round(v * 100) / 100;
  const boxes = {};
  for (const el of document.querySelectorAll('[id]')) {
    const b = el.getBoundingClientRect();
    boxes[el.id] = [r2(b.left + scrollX), r2(b.top + scrollY), r2(b.width), r2(b.height)];
  }
  return boxes;
}

async function main() {
  let names = process.argv.slice(2);
  if (names.length === 0) {
    names = (await fs.readdir(fixtureDir))
      .filter((f) => f.endsWith('.html'))
      .map((f) => f.slice(0, -'.html'.length));
  }
  const browser = await puppeteer.launch({
    executablePath: CHROME,
    headless: 'new',
    args: ['--font-render-hinting=none'],
  });
  try {
    for (const name of names.sort()) {
      const page = await browser.newPage();
      page.on('pageerror', (e) => console.log(`  [pageerror] ${e.message}`));
      page.on('requestfailed', (r) => console.log(`  [requestfailed] ${r.url()} :: ${r.failure()?.errorText}`));
      await page.setViewport(VIEWPORT);
      await page.goto(pathToFileURL(path.join(fixtureDir, `${name}.html`)).toString(), {waitUntil: 'load'});
      const loaded = await page.evaluate(async () => {
        // Load it even on a page with no text, which would never fetch it.
        await document.fonts.load('16px GoRegular');
        return document.fonts.check('16px GoRegular');
      });
      if (!loaded) throw new Error(`${name}: GoRegular did not load — check the @font-face url`);
      const boxes = await page.evaluate(measure);
      await page.close();
      const out = path.join(fixtureDir, `${name}.golden.json`);
      // One line per box keeps golden diffs readable.
      const lines = Object.entries(boxes).map(([id, b]) => `  ${JSON.stringify(id)}: ${JSON.stringify(b)}`);
      await fs.writeFile(out, `{
 "fixture": ${JSON.stringify(name)},
 "chrome": ${JSON.stringify(await browser.version())},
 "viewport": [${VIEWPORT.width}, ${VIEWPORT.height}],
 "boxes": {
${lines.join(',\n')}
 }
}
`);
      console.log(`wrote ${out} (${Object.keys(boxes).length} boxes)`);
    }
  } finally {
    await browser.close();
  }
}

await main();
