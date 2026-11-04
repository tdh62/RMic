import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const root = fileURLToPath(new URL('../../', import.meta.url));
const types = { html: 'text/html', js: 'application/javascript', css: 'text/css', png: 'image/png', webmanifest: 'application/manifest+json' };
const server = createServer(async (req, res) => {
  const name = req.url === '/' ? 'index.html' : req.url.slice(1);
  if (!/^[\w.-]+$/.test(name)) { res.writeHead(404).end(); return; }
  try { res.setHeader('Content-Type', types[name.split('.').pop()] || 'text/plain'); res.end(await readFile(`${root}web/static/${name}`)); }
  catch { res.writeHead(404).end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  const page = await context.newPage(), errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`http://127.0.0.1:${server.address().port}/`);
  await page.locator('#mode-cards').waitFor();
  assert.equal(await page.locator('#connection-badge').textContent(), '待配对');
  assert.equal(await page.locator('#audio-settings').getAttribute('open'), null);
  await page.locator('[data-mode="playback"]').click();
  assert.equal(await page.locator('#mode').inputValue(), 'playback');
  assert.equal(await page.locator('#microphone-meter').isHidden(), true);
  assert.equal(await page.locator('#playback-controls').isVisible(), true);
  assert.equal(await page.locator('#stop').isHidden(), true, 'choosing mode must not start audio');
  await page.locator('#playback-volume').fill('0.7');
  assert.equal(await page.locator('#volume-value').textContent(), '70%');
  await page.locator('[data-mode="duplex"]').focus();
  await page.keyboard.press('Enter');
  assert.equal(await page.locator('#mode').inputValue(), 'duplex');
  assert.equal(await page.locator('#duplex-hint').isVisible(), true);
  // The native selector still controls the same session handler.
  await page.getByLabel('工作模式', { exact: true }).selectOption('mic');
  assert.equal(await page.locator('[data-mode="mic"]').getAttribute('aria-pressed'), 'true');
  await page.evaluate(() => { availableModes = ['mic']; renderMode(); });
  await page.waitForFunction(() => document.querySelector('[data-mode="playback"]').disabled);
  await page.locator('#audio-settings > summary').click();
  await page.locator('#connection').selectOption('ws-pcm');
  await page.waitForFunction(() => $('settings-summary').textContent.includes('PCM'));
  await page.locator('#audio-settings > summary').click();
  await page.evaluate(() => state('正在重新连接', '等待电脑恢复', 'busy'));
  await page.waitForFunction(() => $('connection-badge').dataset.state === 'busy');
  await page.evaluate(() => state('正在传音', 'BlackHole 2ch', 'live'));
  await page.waitForFunction(() => $('connection-badge').textContent === '已连接');
  for (const viewport of [{ width: 320, height: 640 }, { width: 390, height: 844 }, { width: 844, height: 390 }]) {
    await page.setViewportSize(viewport);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `overflow at ${viewport.width}`);
    for (const selector of ['#start', '[data-mode="mic"]', '#request-pair']) {
      const box = await page.locator(selector).boundingBox();
      assert.ok(box.height >= 44 && box.width >= 44, `${selector} touch target too small`);
    }
  }
  // Expanded settings, long messages and larger text must also fit small screens.
  await page.evaluate(() => {
    document.querySelectorAll('details').forEach(node => { node.open = true; });
    state('正在重新连接', 'https://a-long-computer-name.example.local:8443/ · 等待电脑恢复连接', 'busy');
    showError('连接暂时中断，请检查电脑和网络后重试。');
  });
  for (const width of [280, 320, 360, 390]) {
    await page.setViewportSize({ width, height: 640 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `expanded settings overflow at ${width}`);
  }
  await page.setViewportSize({ width: 320, height: 640 });
  await page.evaluate(() => {
    const sizes = [...document.querySelectorAll('main *')].map(node => [node, parseFloat(getComputedStyle(node).fontSize)]);
    for (const [node, size] of sizes) node.style.fontSize = `${size * 2}px`;
  });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'large text overflows narrow screen');
  await page.evaluate(() => {
    document.querySelectorAll('main *').forEach(node => { node.style.fontSize = ''; });
    document.querySelectorAll('details').forEach(node => { node.open = false; });
    $('error').hidden = true;
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  assert.equal(await page.locator('#level').evaluate(node => getComputedStyle(node).transitionDuration), '0s');
  await page.evaluate(() => { $('pairing').hidden = true; availableModes = ['mic', 'playback', 'duplex']; $('connection').value = 'webrtc'; renderQuality(); renderMode(); state('尚未连接', ''); });
  await mkdir(`${root}artifacts`, { recursive: true });
  await page.screenshot({ path: `${root}artifacts/mobile-redesign.png`, fullPage: true });
  assert.deepEqual(errors, []);
  console.log('PASS mobile mode cards and native selector, keyboard, capability restrictions, volume feedback, status, touch targets, narrow/landscape layout and reduced motion');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
