// Drives system Chrome over puppeteer-core to render each raw HTML form
// control from native.html and writes one PNG per case to
// /tmp/html-native-ref/. Each PNG is sized to the element rect + 16 px
// padding on every side, on a white page, so the AE diff against qui's
// /tmp/qui-html/<case>.png snapshot is meaningful.
//
// Usage:
//   node widgets/scripts/web/capture.mjs
// Requires: puppeteer-core, Google Chrome at the macOS default path.

import puppeteer from '/opt/js_work/material-web/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js';
import {fileURLToPath, pathToFileURL} from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const indexUrl = pathToFileURL(path.join(__dirname, 'native.html')).toString();
const outDir = '/tmp/html-native-ref';
await fs.mkdir(outDir, {recursive: true});

const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const PAD = 16; // matches widgets/html_golden_test.go's `pad` constant

// (widget, variant, fileBase) tuples. fileBase must match the `cases`
// table in widgets/html_golden_test.go so compare-html.sh can diff
// /tmp/qui-html/<base>.png against /tmp/html-native-ref/<base>.png.
const cases = [
  {widget: 'input',    variant: 'text',      base: 'input-text'},
  {widget: 'textarea', variant: 'text',      base: 'textarea'},
  {widget: 'select',   variant: 'default',   base: 'select'},
  {widget: 'button',   variant: 'default',   base: 'button'},
  {widget: 'checkbox', variant: 'unchecked', base: 'checkbox-unchecked'},
  {widget: 'checkbox', variant: 'checked',   base: 'checkbox-checked'},
  {widget: 'radio',    variant: 'unchecked', base: 'radio-unchecked'},
  {widget: 'radio',    variant: 'checked',   base: 'radio-checked'},
];

const browser = await puppeteer.launch({
  executablePath: CHROME,
  headless: 'new',
  args: ['--disable-features=PartialRasterInvalidation', '--font-render-hinting=none'],
});
try {
  for (const c of cases) {
    const params = new URLSearchParams({widget: c.widget, v: c.variant});
    const url = `${indexUrl}?${params.toString()}`;
    const page = await browser.newPage();
    page.on('console', m => console.log(`  [page.${m.type()}] ${m.text()}`));
    page.on('pageerror', e => console.log(`  [pageerror] ${e.message}`));
    page.on('requestfailed', r => console.log(`  [requestfailed] ${r.url()} :: ${r.failure()?.errorText}`));
    await page.setViewport({width: 480, height: 240, deviceScaleFactor: 1});
    await page.goto(url, {waitUntil: 'load'});
    await page.waitForFunction('window.__qui_ready__ === true', {timeout: 15000});
    const bbox = await page.evaluate('window.__qui_bbox__');
    const w = Math.round(bbox.w);
    const h = Math.round(bbox.h);
    const clip = {x: 0, y: 0, width: w + PAD * 2, height: h + PAD * 2};
    const out = path.join(outDir, `${c.base}.png`);
    await page.screenshot({path: out, clip, omitBackground: false});
    console.log(`wrote ${out} ${w}×${h} (${c.widget}) → ${clip.width}×${clip.height} (canvas)`);
    await page.close();
  }
} finally {
  await browser.close();
}
