// Records the DOM's event ordering for each fixture under
// htmlcss/testdata/events/ in real Chrome, writing <name>.golden.json next
// to the fixture. htmlcss/dom_event_order_test.go replays the same input
// sequence through qui and diffs its log against these goldens, so the
// goldens are committed and `go test` never needs Chrome.
//
// Each fixture is a pair:
//   <name>.html        — plain HTML + <style>, parsed as-is by htmlcss.
//   <name>.steps.json  — the input script: [{do, on?, key?, dy?}, ...]
//                        do ∈ click | dblclick | rightclick | wheel | key.
//                        Pointer steps aim at the center of #<on>; key steps
//                        press one key on whatever has focus.
//
// Every element with an id gets a non-capturing listener for each of
// LOGGED below; each step's log is the ordered list of "<type> <id> <phase>"
// entries it produced (phase: target | bubble), followed by the post-step
// state as pseudo-entries — the default actions the step had:
//   "@focus <id>"          document.activeElement ("-" for body)
//   "@value <id> <json>"   each form control's value (checked for boxes)
// A <form> submit is logged as "submit <form-id> target" (and prevented,
// so the page stays put).
//
// Regenerate (needs Chrome + puppeteer-core from /opt/js_work/material-web,
// same setup as compare-html.sh / capture.mjs):
//   REFRESH_REF=1 go test ./htmlcss -run DOMEventOrder
// which runs: node widgets/scripts/web/dom-events.mjs [fixture-name ...]

import puppeteer from '/opt/js_work/material-web/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js';
import {fileURLToPath, pathToFileURL} from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const fixtureDir = path.resolve(__dirname, '../../../htmlcss/testdata/events');
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const VIEWPORT = {width: 480, height: 360, deviceScaleFactor: 1};

const LOGGED = ['keydown', 'keyup', 'pointerdown', 'pointerup', 'click',
  'dblclick', 'contextmenu', 'wheel', 'focus', 'blur'];

// Installed in the page after load. Kept self-contained: it is serialized.
function installLogger(logged) {
  window.__log = [];
  for (const el of document.querySelectorAll('[id]')) {
    for (const type of logged) {
      el.addEventListener(type, (e) => {
        window.__log.push(`${type} ${el.id} ${e.eventPhase === 2 ? 'target' : 'bubble'}`);
      });
    }
    if (el.tagName === 'FORM') {
      el.addEventListener('submit', (e) => {
        e.preventDefault();
        window.__log.push(`submit ${el.id} target`);
      });
    }
  }
}

function takeLog() {
  const out = window.__log.slice();
  window.__log.length = 0;
  const a = document.activeElement;
  out.push(`@focus ${a && a !== document.body && a.id ? a.id : '-'}`);
  for (const el of document.querySelectorAll('input[id], textarea[id], select[id]')) {
    const v = el.type === 'checkbox' || el.type === 'radio' ? String(el.checked) : el.value;
    out.push(`@value ${el.id} ${JSON.stringify(v)}`);
  }
  return out;
}

const puppeteerKey = {Space: 'Space', Enter: 'Enter', Tab: 'Tab', Escape: 'Escape'};

async function center(page, id) {
  const r = await page.evaluate((id) => {
    const el = document.getElementById(id);
    if (!el) return null;
    const b = el.getBoundingClientRect();
    return {x: b.left + b.width / 2, y: b.top + b.height / 2};
  }, id);
  if (!r) throw new Error(`no element #${id}`);
  return r;
}

async function runStep(page, step) {
  switch (step.do) {
    case 'click':
    case 'dblclick':
    case 'rightclick': {
      const {x, y} = await center(page, step.on);
      const opts = step.do === 'dblclick' ? {count: 2}
        : step.do === 'rightclick' ? {button: 'right'} : {};
      await page.mouse.click(x, y, opts);
      break;
    }
    case 'wheel': {
      const {x, y} = await center(page, step.on);
      await page.mouse.move(x, y);
      await page.mouse.wheel({deltaY: step.dy ?? 40});
      break;
    }
    case 'key':
      await page.keyboard.press(puppeteerKey[step.key] ?? step.key);
      break;
    default:
      throw new Error(`unknown step ${JSON.stringify(step)}`);
  }
  // Let async dispatch (wheel, focus) settle, and keep consecutive clicks
  // out of the double-click window.
  await new Promise((r) => setTimeout(r, 600));
}

async function main() {
  let names = process.argv.slice(2);
  if (names.length === 0) {
    names = (await fs.readdir(fixtureDir))
      .filter((f) => f.endsWith('.steps.json'))
      .map((f) => f.slice(0, -'.steps.json'.length));
  }
  const browser = await puppeteer.launch({executablePath: CHROME, headless: 'new'});
  try {
    for (const name of names.sort()) {
      const steps = JSON.parse(await fs.readFile(path.join(fixtureDir, `${name}.steps.json`), 'utf8'));
      const page = await browser.newPage();
      page.on('pageerror', (e) => console.log(`  [pageerror] ${e.message}`));
      await page.setViewport(VIEWPORT);
      await page.goto(pathToFileURL(path.join(fixtureDir, `${name}.html`)).toString(), {waitUntil: 'load'});
      await page.evaluate(installLogger, LOGGED);
      const log = [];
      for (const step of steps) {
        await runStep(page, step);
        log.push({step, entries: await page.evaluate(takeLog)});
      }
      await page.close();
      const out = path.join(fixtureDir, `${name}.golden.json`);
      await fs.writeFile(out, JSON.stringify({
        fixture: name,
        chrome: await browser.version(),
        log,
      }, null, 1) + '\n');
      console.log(`wrote ${out} (${steps.length} steps)`);
    }
  } finally {
    await browser.close();
  }
}

await main();
