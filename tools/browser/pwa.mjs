import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const assets = fileURLToPath(new URL('../../web/static/', import.meta.url));
const types = { html: 'text/html', js: 'application/javascript', css: 'text/css', png: 'image/png', webmanifest: 'application/manifest+json' };
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname;
  const name = path === '/' ? 'index.html' : path.slice(1);
  if (!/^[\w.-]+$/.test(name)) { res.writeHead(404).end(); return; }
  try { res.setHeader('Content-Type', types[name.split('.').pop()] || 'text/plain'); res.end(await readFile(`${assets}${name}`)); }
  catch { res.writeHead(404).end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch({ channel: 'chromium', headless: true });
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
  await context.addInitScript(() => {
    window.wakeDenied = false;
    Object.defineProperty(navigator, 'wakeLock', { value: { request: async () => {
      if (window.wakeDenied) throw new Error('denied');
      const lock = new EventTarget(); lock.released = false;
      lock.release = async () => { lock.released = true; lock.dispatchEvent(new Event('release')); };
      window.testWake = lock; return lock;
    } } });
  });
  const page = await context.newPage(), errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const url = `http://127.0.0.1:${server.address().port}/`;
  await page.goto(url);
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  const manifest = await page.evaluate(async () => (await fetch(document.querySelector('link[rel="manifest"]').href)).json());
  assert.equal(manifest.display, 'standalone'); assert.equal(manifest.start_url, '/');
  for (const icon of manifest.icons) {
    const response = await page.request.get(new URL(icon.src, url).href);
    assert.equal(response.status(), 200);
    const png = await response.body();
    assert.equal(png.readUInt32BE(16), Number(icon.sizes.split('x')[0]));
  }
  assert.equal(await page.locator('#black-screen').isDisabled(), true);
  // Use a synthetic active session to isolate display lifecycle from audio hardware.
  await page.evaluate(() => { wanted = true; current = { abort: new AbortController() }; $('black-screen').disabled = false; });
  await page.locator('#black-screen').click();
  await page.waitForFunction(() => $('black-overlay').open && !!current.wake);
  assert.equal(await page.locator('#black-overlay').evaluate(el => getComputedStyle(el).backgroundColor), 'rgb(0, 0, 0)');
  assert.equal(await page.evaluate(() => wanted && !current.abort.signal.aborted), true);
  await page.locator('#exit-black-screen').click({ position: { x: 20, y: 20 } });
  assert.equal(await page.locator('#black-overlay').evaluate(el => el.open), false);
  assert.equal(await page.evaluate(() => wanted && !current.wake.released), true, 'exit preserves session and wake lock');
  await page.locator('#black-screen').click();
  await page.keyboard.press('Escape');
  await page.waitForFunction(() => !$('black-overlay').open);
  await page.locator('#black-screen').click();
  await page.evaluate(() => window.testWake.release());
  await page.waitForFunction(() => !$('black-overlay').open);
  assert.match(await page.locator('#display-hint').textContent(), /常亮已失效/);
  await page.evaluate(() => { window.wakeDenied = true; });
  await page.locator('#black-screen').click();
  await page.waitForFunction(() => !$('black-overlay').open);
  assert.match(await page.locator('#display-hint').textContent(), /无法保持屏幕常亮/);
  await page.evaluate(() => { window.wakeDenied = false; });
  await page.locator('#black-screen').click();
  await page.evaluate(() => stop());
  assert.equal(await page.locator('#black-overlay').evaluate(el => el.open), false);
  assert.equal(await page.locator('#black-screen').isDisabled(), true);
  await page.evaluate(() => {
    const event = new Event('beforeinstallprompt', { cancelable: true });
    event.prompt = async () => { window.installRequested = true; };
    event.userChoice = Promise.resolve({ outcome: 'accepted' });
    window.dispatchEvent(event);
  });
  await page.locator('#install-app').click();
  assert.equal(await page.evaluate(() => window.installRequested), true);
  assert.equal(await page.locator('#install-app').isHidden(), true);
  await page.locator('#certificate-help summary').click();
  const recoveryResponse = page.waitForResponse(response => response.request().isNavigationRequest() && new URL(response.url()).searchParams.has('rmic-trust'));
  await page.locator('#trust-certificate').click();
  assert.equal((await recoveryResponse).fromServiceWorker(), false, 'certificate recovery goes directly to the browser network stack');
  await page.waitForFunction(() => !location.search);
  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  await context.setOffline(true);
  await page.evaluate(async () => { try { await api('/api/status'); } catch (_) {} });
  assert.equal(await page.locator('#certificate-help').evaluate(el => el.open), true, 'network failures expose certificate recovery');
  await page.goto(url);
  assert.match(await page.locator('body').textContent(), /无法连接电脑/);
  const failedRecovery = page.waitForEvent('requestfailed', request => request.isNavigationRequest() && new URL(request.url()).searchParams.has('rmic-trust'));
  await page.getByRole('link', { name: '重新信任证书' }).click();
  await failedRecovery;
  await context.setOffline(false);
  await page.goto(url);
  assert.deepEqual(errors, []);
  console.log('PASS PWA manifest/icons, service worker offline fallback, install action, black screen, wake lock failure/release, exit and stop');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
