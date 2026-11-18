// Shared Chrome launcher for the reference harnesses in this directory.
//
// puppeteer-core is this directory's own pinned dependency (package.json +
// package-lock.json); install it once with `npm ci` here. It drives an
// installed Chrome rather than downloading one: CHROME_BIN names the
// executable, defaulting to the macOS install path.

import {createRequire} from 'node:module';
import path from 'node:path';
const require = createRequire(process.env.QUI_WEB_DIR
  ? path.resolve(process.env.QUI_WEB_DIR, 'package.json') : import.meta.url);
const puppeteer = require('puppeteer-core');

export const CHROME = process.env.CHROME_BIN
  || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

export async function launchChrome(args = []) {
  if (process.env.CHROME_URL) {
    const endpoint = new URL(process.env.CHROME_URL);
    if (!['http:', 'https:'].includes(endpoint.protocol) || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw new Error('invalid Chrome DevTools browser URL');
    const browser = await puppeteer.connect({browserURL: endpoint.href, defaultViewport: null});
    try {
      const context = await browser.createBrowserContext();
      return {
        newPage: () => context.newPage(),
        version: () => browser.version(),
        close: async () => { try { await context.close(); } finally { browser.disconnect(); } },
      };
    } catch (e) { browser.disconnect(); throw e; }
  }
  return puppeteer.launch({executablePath: CHROME, headless: 'new', args});
}
