// Shared Chrome launcher for the reference harnesses in this directory.
//
// puppeteer-core is this directory's own pinned dependency (package.json +
// package-lock.json); install it once with `npm ci` here. It drives an
// installed Chrome rather than downloading one: CHROME_BIN names the
// executable, defaulting to the macOS install path.

import puppeteer from 'puppeteer-core';

export const CHROME = process.env.CHROME_BIN
  || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

export function launchChrome(args = []) {
  return puppeteer.launch({executablePath: CHROME, headless: 'new', args});
}
