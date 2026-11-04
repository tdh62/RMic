import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { createSocket } from 'node:dgram';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const root = fileURLToPath(new URL('../../', import.meta.url));
const config = await mkdtemp(`${tmpdir()}/rmic-desktop-`);
const pulse = args => execFileSync('pactl', args, { encoding: 'utf8' }).trim();
const modules = [];
const prefix = `rmic_lifecycle_${process.pid}`;
const token = randomBytes(24).toString('hex'), deviceID = randomBytes(24).toString('hex');
const authorization = { version: 1, token, device_id: deviceID };
const reservation = createServer();
await new Promise(resolve => reservation.listen(0, '127.0.0.1', resolve));
const port = reservation.address().port;
await new Promise(resolve => reservation.close(resolve));
const udpReservation = createSocket('udp4');
await new Promise(resolve => udpReservation.bind(0, '127.0.0.1', resolve));
const udpPort = udpReservation.address().port;
await new Promise(resolve => udpReservation.close(resolve));
let app, browser, page, output = '', success = false;
const discoveredDevice = new RegExp(String.raw`(?:麦克风送入：${prefix}_mic[12]|电脑声音来源：Monitor of ${prefix}_computer)\s+`);
const errors = [];
async function until(check, description) {
  for (let i = 0; i < 250; i++) {
    if (await check()) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`Timed out: ${description}\n${output.slice(-6000)}`);
}
async function edit(field, keys) {
  const marker = output.length;
  app.stdin.write('c');
  await until(() => output.slice(marker).includes('当前服务保持运行'), 'live editor');
  await until(() => discoveredDevice.test(output.slice(marker)), 'configuration device discovery');
  for (const key of 's'.repeat(field) + keys) { app.stdin.write(key); await new Promise(resolve => setTimeout(resolve, 20)); }
}
async function apply() { app.stdin.write('g'); }
async function status(page) { return page.evaluate(() => api('/api/status')); }
async function ready(page, mode) {
  await page.waitForFunction(mode => wanted && current?.ready && current.mode === mode && ['正在传音', '正在播放', '双向传音中'].includes($('status').textContent), mode, { timeout: 30000 });
}
try {
  for (const [name, channels] of [[`${prefix}_mic1`, 2], [`${prefix}_mic2`, 2], [`${prefix}_computer`, 6]]) {
    modules.push(pulse(['load-module', 'module-null-sink', `sink_name=${name}`, `channels=${channels}`, 'rate=48000', `sink_properties=device.description=${name}`, `channel_map=${channels === 6 ? 'front-left,front-right,front-center,lfe,side-left,side-right' : 'front-left,front-right'}`]));
  }
  await writeFile(`${config}/desktop.json`, JSON.stringify({ mode: 'duplex', default_mode: 'mic', output: `${prefix}_mic1`, capture: `Monitor of ${prefix}_computer`, quality: 'balanced', network: '127.0.0.1', https_port: port, udp_port: udpPort, capture_channels: 2 }));
  await writeFile(`${config}/trusted-devices.json`, JSON.stringify({ version: 1, devices: [{ id: deviceID, name: 'Lifecycle browser', created: new Date().toISOString(), remember: true, token_hash: createHash('sha256').update(token).digest('hex') }] }));
  app = spawn('script', ['-qfec', 'stty rows 45 cols 160; exec "$RMIC_BINARY" --setup --config-dir="$RMIC_CONFIG"', '/dev/null'], { cwd: root, env: { ...process.env, TERM: 'xterm-256color', RMIC_BINARY: `${root}bin/rmic`, RMIC_CONFIG: config } });
  app.stdin.on('error', () => {});
  app.stdout.on('data', chunk => { output += chunk; if (chunk.includes(Buffer.from('\x1b[6n'))) app.stdin.write('\x1b[1;1R'); });
  app.stderr.on('data', chunk => { output += chunk; });
  await until(() => output.includes('启动配置'), 'startup editor');
  await until(() => discoveredDevice.test(output), 'startup device discovery');
  app.stdin.write('g');
  await until(() => output.includes('等待设备'), 'running service');
  browser = await chromium.launch({ headless: true, args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'] });
  const context = await browser.newContext({ ignoreHTTPSErrors: true, permissions: ['microphone'] });
  await context.addInitScript(auth => {
    localStorage.setItem('rmic-device-auth', JSON.stringify(auth));
    const originalTimeout = window.setTimeout.bind(window);
    window.setTimeout = (callback, delay, ...args) => originalTimeout(callback, window.fastRetry && [1000, 2000, 4000, 8000].includes(delay) && document.getElementById('status')?.textContent === '正在重新连接' ? 10 : delay, ...args);
  }, authorization);
  page = await context.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`https://127.0.0.1:${port}/`);
  await page.locator('#start').click();
  await ready(page, 'mic');
  // A pending media request from a previous attempt must not block Start.
  await page.locator('#stop').click();
  await page.evaluate(() => {
    const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async options => {
      navigator.mediaDevices.getUserMedia = original;
      window.lateStream = await original(options);
      return new Promise(resolve => { window.finishOldMedia = () => resolve(window.lateStream); });
    };
  });
  await page.locator('#start').click();
  await page.waitForFunction(() => !!window.finishOldMedia);
  await page.locator('#stop').click();
  await page.locator('#start').click();
  await ready(page, 'mic');
  const restartedID = await page.evaluate(() => current.id);
  await page.evaluate(() => window.finishOldMedia());
  await page.waitForFunction(() => window.lateStream.getTracks().every(track => track.readyState === 'ended'));
  assert.equal(await page.evaluate(() => current.id), restartedID);
  console.log('PASS Start recovers from a stalled media request without reloading and releases late microphone tracks');

  // Some browsers remain disconnected without ever firing failed.
  await page.evaluate(() => {
    Object.defineProperty(current.pc, 'connectionState', { configurable: true, get: () => 'disconnected' });
    current.pc.onconnectionstatechange();
  });
  await page.waitForFunction(id => current?.ready && current.id !== id, restartedID, { timeout: 30000 });
  await ready(page, 'mic');
  console.log('PASS prolonged WebRTC disconnection rebuilds the session automatically');
  const first = await page.evaluate(() => ({ id: current.id, instance: current.instance, revision: current.revision }));
  await edit(0, '');
  assert.equal((await status(page)).state, 'streaming');
  app.stdin.write('\x1b');
  await page.waitForTimeout(300);
  assert.equal(await page.evaluate(() => current.id), first.id);
  console.log('PASS opening and cancelling live configuration keeps HTTPS and active audio session');

  await edit(3, 'd');
  await apply();
  await until(async () => (await status(page)).default_quality.id === 'music', 'default quality update');
  assert.equal(await page.evaluate(() => current.id), first.id);
  assert.equal(await page.locator('#quality').inputValue(), 'balanced');
  console.log('PASS default quality update preserves active session and phone preference');

  await edit(1, 'd');
  await apply();
  await page.waitForFunction(id => current?.ready && current.id !== id, first.id, { timeout: 30000 });
  await ready(page, 'mic');
  const replaced = await status(page);
  assert.equal(replaced.instance_id, first.instance);
  assert.equal(replaced.config_revision, first.revision + 1);
  assert.equal(replaced.receive.device, `${prefix}_mic2`);
  console.log('PASS audio device replacement preserves HTTPS, authorization and reconnects WebRTC');

  await page.evaluate(() => { for (const mode of ['playback', 'duplex', 'mic', 'playback']) { $('mode').value = mode; $('mode').dispatchEvent(new Event('change')); } });
  await ready(page, 'playback');
  await page.locator('#audio-settings > summary').click();
  await page.locator('#connection').selectOption('ws-pcm');
  await ready(page, 'playback');
  await page.locator('#pcm-output').selectOption('stereo');
  await ready(page, 'playback');
  const beforeChannels = await page.evaluate(() => current.id);
  await edit(7, 'd');
  await apply();
  await page.waitForFunction(id => current?.ready && current.id !== id && availableCaptureChannels === 6, beforeChannels, { timeout: 30000 });
  await ready(page, 'playback');
  console.log('PASS rapid mode changes and PCM renegotiation after 2→6 channel configuration');

  // Exceed the old five-attempt limit without spending a minute in backoff.
  await page.evaluate(() => { window.fastRetry = true; });
  await page.route('**/api/status', route => route.abort('connectionrefused'));
  await page.waitForFunction(() => retries >= 7, null, { timeout: 20000 });
  assert.equal(await page.evaluate(() => wanted), true);
  await page.unroute('**/api/status');
  await page.evaluate(() => window.dispatchEvent(new Event('online')));
  await ready(page, 'playback');
  assert.equal(await page.locator('#quality').inputValue(), 'balanced');
  console.log('PASS recovery beyond five failed attempts refreshes capabilities and preserves selected quality');

  const beforeNetwork = (await status(page)).instance_id;
  await edit(6, '\r\x15' + String(udpPort === 65535 ? udpPort - 1 : udpPort + 1) + '\r');
  await apply();
  await page.waitForFunction(instance => current?.ready && current.instance !== instance, beforeNetwork, { timeout: 30000 });
  await ready(page, 'playback');
  assert.equal(await page.locator('#pairing').isHidden(), true);
  console.log('PASS controlled UDP listener restart preserves authorization and restores same-address phone session');

  const beforeRollback = (await status(page)).instance_id;
  const occupied = createServer();
  await new Promise(resolve => occupied.listen(0, '127.0.0.1', resolve));
  try {
    await edit(5, '\r\x15' + String(occupied.address().port) + '\r');
    await apply();
    await page.waitForFunction(instance => current?.ready && current.instance !== instance, beforeRollback, { timeout: 30000 });
    await ready(page, 'playback');
    await until(() => output.includes('已恢复原配置'), 'failed listener rollback');
  } finally { await new Promise(resolve => occupied.close(resolve)); }
  console.log('PASS failed listener binding rolls back to last running configuration');

  // Restricting capabilities must not silently add microphone capture.
  await edit(0, 'd'); // duplex → mic
  await apply();
  await page.waitForFunction(() => !wanted && $('status').textContent === '连接已停止', null, { timeout: 30000 });
  assert.equal(await page.locator('#mode option[value="playback"]').isDisabled(), true);
  assert.equal(await page.locator('#pairing').isHidden(), true);
  assert.match(await page.locator('#error').textContent(), /重新选择工作模式/);
  await page.locator('#mode').selectOption('mic');
  await page.locator('#start').click();
  await ready(page, 'mic');
  app.stdin.write('D');
  await page.waitForFunction(() => !wanted && $('status').textContent === '电脑端已断开');
  await page.waitForTimeout(500);
  assert.equal(await page.evaluate(() => current), null);
  assert.deepEqual(errors, []);
  console.log('PASS unavailable direction needs explicit selection and computer disconnect stops automatic recovery');
  await edit(0, 'd'); // mic → playback, while the phone is idle.
  await apply();
  await page.waitForFunction(() => availableModes.length === 1 && availableModes[0] === 'playback', null, { timeout: 15000 });
  assert.equal(await page.evaluate(() => wanted || current !== null), false);
  assert.equal(await page.locator('#mode').inputValue(), 'playback');
  await page.locator('#start').click();
  await ready(page, 'playback');
  assert.deepEqual(errors, []);
  console.log('PASS idle phone refreshes newly permitted modes without starting audio automatically');
  success = true;
} finally {
  if (!success && page && !page.isClosed()) {
    console.error('Client at failure:', await page.evaluate(() => ({ wanted, retries, mode: $('mode').value, status: $('status').textContent, error: $('error').textContent, channels: availableCaptureChannels, current: current && { id: current.id, mode: current.mode, ready: current.ready, revision: current.revision, transport: current.transport } })).catch(() => null));
  }
  await writeFile('/tmp/rmic-desktop-tui.log', output);
  await browser?.close();
  if (app?.exitCode === null) { app.stdin.write(''); await new Promise(resolve => setTimeout(resolve, 100)); app.stdin.write('q'); try { await until(() => app.exitCode !== null, 'desktop shutdown'); } catch { app.kill('SIGTERM'); } }
  for (const module of modules.reverse()) { try { pulse(['unload-module', module]); } catch {} }
  await rm(config, { recursive: true, force: true });
}
