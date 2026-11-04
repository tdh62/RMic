import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

const html = `<!doctype html><html><body>
<section id="shortcut-controls" hidden><div id="shortcut-buttons"></div><p id="shortcut-state"></p><button id="shortcut-release">释放</button></section>
<script src="/shortcuts.js"></script>
<script>
window.messages = [];
class FakeSocket {
  static OPEN = 1;
  constructor() { this.readyState = 1; window.socket = this; queueMicrotask(() => this.onmessage?.({data: JSON.stringify({type: 'ready'})})); }
  send(raw) {
    const message = JSON.parse(raw); window.messages.push(message);
    queueMicrotask(() => this.onmessage?.({data: JSON.stringify({type:'ack', seq:message.seq, ...(window.rejectHold && message.type === 'button.hold_begin' ? {error:'注入失败'} : {})})}));
  }
  close() { this.readyState = 3; }
}
window.WebSocket = FakeSocket;
window.controls = new RmicShortcuts(async () => ({ticket:'once'}));
window.config = {revision:1,buttons:[{id:'1',name:'保持组合',mode:'hold'},{id:'2',name:'固定回复',mode:'tap'},{id:'3',name:'另一组合',mode:'hold'}]};
</script></body></html>`;
const server = createServer(async (req, res) => {
  if (req.url === '/shortcuts.js') {
    res.setHeader('Content-Type', 'text/javascript');
    res.end(await readFile(new URL('../../web/static/shortcuts.js', import.meta.url)));
  } else { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(html); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch({headless:true});
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`http://127.0.0.1:${server.address().port}/`);
  assert.equal(await page.locator('#shortcut-controls').isVisible(), false);
  await page.evaluate(() => controls.update(config, 'audio-1'));
  await page.waitForFunction(() => controls.ready);
  assert.equal(await page.locator('.shortcut-button').count(), 3);
  const hold = page.locator('[data-id="1"]');
  await hold.hover();
  await page.mouse.down();
  await page.waitForFunction(() => document.querySelector('[data-id="1"]').classList.contains('holding'));
  assert.equal(await page.locator('[data-id="2"]').isDisabled(), true);
  await page.mouse.up();
  await page.waitForFunction(() => messages.some(message => message.type === 'button.hold_end'));
  assert.equal(await hold.getAttribute('aria-pressed'), 'false');

  await hold.focus();
  await page.keyboard.down('Space');
  await page.waitForFunction(() => controls.holds.size === 1);
  await page.keyboard.up('Space');
  await page.waitForFunction(() => controls.holds.size === 0);

  // Pointer cancellation and multiple holds must not leave a local latch.
  await page.evaluate(() => {
    controls.begin(document.querySelector('[data-id="1"]'), 91);
    controls.begin(document.querySelector('[data-id="3"]'), 92);
  });
  await page.waitForFunction(() => document.querySelectorAll('.holding').length === 2);
  await hold.dispatchEvent('pointercancel', {pointerId:91});
  assert.equal(await page.evaluate(() => controls.holds.size), 1);
  await page.locator('[data-id="3"]').dispatchEvent('lostpointercapture', {pointerId:92});
  assert.equal(await page.evaluate(() => controls.holds.size), 0);

  await page.locator('[data-id="2"]').click();
  assert.equal(await hold.isDisabled(), true);
  await page.evaluate(() => socket.onmessage({data:JSON.stringify({type:'complete'})}));
  assert.equal(await hold.isDisabled(), false);

  await page.evaluate(() => { window.rejectHold = true; });
  await hold.hover(); await page.mouse.down();
  await page.waitForFunction(() => document.getElementById('shortcut-state').textContent === '注入失败');
  assert.equal(await page.evaluate(() => controls.holds.size), 0);
  await page.mouse.up();
  await page.evaluate(() => controls.update(config, 'audio-1'));
  await page.waitForFunction(() => controls.ready);
  await page.evaluate(() => { window.rejectHold = false; controls.begin(document.querySelector('[data-id="1"]'), 93); });
  await page.waitForFunction(() => controls.holds.size === 1);
  await page.evaluate(() => window.dispatchEvent(new Event('blur')));
  assert.equal(await page.evaluate(() => controls.holds.size), 0);
  assert.equal(await hold.isDisabled(), true);
  const begins = await page.evaluate(() => messages.filter(message => message.type === 'button.hold_begin').length);
  await page.evaluate(() => controls.update(config, 'audio-1'));
  await page.waitForFunction(() => controls.ready);
  assert.equal(await page.evaluate(() => messages.filter(message => message.type === 'button.hold_begin').length), begins);

  await page.evaluate(() => controls.update(null, 'audio-1'));
  assert.equal(await page.locator('#shortcut-controls').isVisible(), false);
  assert.equal(await page.evaluate(() => controls.ws), null);
  assert.deepEqual(errors, []);
  console.log('PASS shortcut visibility, pointer/keyboard holds, shared controls, cancellation, run state, rejection and no replay');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
