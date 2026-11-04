import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdir, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const root = fileURLToPath(new URL('../../', import.meta.url));
const config = await mkdtemp(`${tmpdir()}/rmic-browser-`);
const app = spawn(`${root}/bin/rmic`, ['--headless', '--ip=127.0.0.1', '--listen=:0', '--udp-port=0', `--config-dir=${config}`], { cwd: root });
const errors = [];
let browser, producer, feedTimer, loopback, startup = '';
const status = (page, text) => page.locator('#status').filter({ hasText: new RegExp(`^${text}$`) }).waitFor({ timeout: 25000 });
const pulse = args => execFileSync('pactl', args, { encoding: 'utf8', env: { ...process.env, LC_ALL: 'C' } }).trim();
try {
  const url = await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(`RMic startup timeout: ${startup}`)), 15000);
    app.stdout.on('data', chunk => {
      startup += chunk;
      const match = startup.match(/https:\/\/127\.0\.0\.1:\d+\/#token=[a-f0-9]+/);
      if (match) { clearTimeout(timeout); resolve(match[0]); }
    });
    app.stderr.on('data', chunk => { startup += chunk; });
    app.on('error', reject);
    app.on('exit', code => { clearTimeout(timeout); reject(new Error(`RMic exited ${code}: ${startup}`)); });
  });
  assert.throws(() => execFileSync(`${root}/bin/rmic`, ['--virtual=off', '--device=RMic_Virtual_Output', '--capture=RMic_Virtual_Microphone', '--ip=127.0.0.1'], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }), error => error.status !== 0 && error.stderr.includes('反馈路径'));
  console.log('PASS unsafe same-cable routing is rejected at startup');
  producer = spawn('pacat', ['--playback', '--raw', '--format=s16le', '--rate=48000', '--channels=2', '--device=rmic_computer']);
  producer.stdin.on('error', () => {});
  let position = 0;
  feedTimer = setInterval(() => {
    if (producer.stdin.destroyed || producer.stdin.writableNeedDrain) return;
    const frame = Buffer.alloc(3840);
    for (let i = 0; i < 960; i++) {
      const value = Math.round(10000 * Math.sin(2 * Math.PI * 880 * (position + i) / 48000));
      frame.writeInt16LE(value, i * 4); frame.writeInt16LE(value, i * 4 + 2);
    }
    position += 960; producer.stdin.write(frame);
  }, 20);

  const interfaceInfo = execFileSync(`${root}/bin/rmic`, ['--list-interfaces'], { encoding: 'utf8' });
  assert.match(interfaceInfo, /default=true/);
  browser = await chromium.launch({ headless: true, args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'] });
  const first = await browser.newContext({ ignoreHTTPSErrors: true, permissions: ['microphone'], viewport: { width: 390, height: 844 } });
  await first.addInitScript(() => {
    window.testStreams = []; window.microphoneRequests = 0;
    const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async (...args) => { window.microphoneRequests++; const stream = await original(...args); window.testStreams.push(stream); return stream; };
  });
  const page = await first.newPage();
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(url);
  assert.equal(new URL(page.url()).hash, '', 'pairing token must leave the address bar');
  if (interfaceInfo.includes('::1')) {
    const ipv6 = await first.newPage();
    await ipv6.goto(url.replace('127.0.0.1', '[::1]'));
    assert.equal(await ipv6.evaluate(async () => (await api('/api/status')).network.some(address => address.startsWith('[::1]:'))), true);
    await ipv6.close();
    console.log('PASS multi-address HTTPS and IPv6 API listener');
  }
  await page.getByRole('button', { name: '开始传音' }).click();
  await status(page, '正在传音');
  await page.locator('#directions > summary').click();
  await page.getByRole('button', { name: '静音', exact: true }).click();
  await status(page, '已静音');
  await page.getByRole('button', { name: '取消静音', exact: true }).click();
  await status(page, '正在传音');
  console.log('PASS microphone mode and independent microphone mute');

  const second = await browser.newContext({ ignoreHTTPSErrors: true, permissions: ['microphone'] });
  const other = await second.newPage();
  other.on('pageerror', error => errors.push(error.message));
  await other.goto(url);
  await other.getByRole('button', { name: '开始传音' }).click();
  await other.getByRole('alert').filter({ hasText: '占用' }).waitFor({ timeout: 20000 });
  console.log('PASS second device cannot replace active session');
  await first.setOffline(true);
  await status(page, '正在重新连接');
  await first.setOffline(false);
  await status(page, '正在传音');
  console.log('PASS reconnect after network interruption');

  const requests = await page.evaluate(() => window.microphoneRequests);
  await page.getByLabel('工作模式').selectOption('playback');
  await status(page, '正在播放');
  assert.equal(await page.evaluate(() => window.microphoneRequests), requests, 'playback must not request microphone');
  assert.equal(await page.evaluate(() => window.testStreams.every(s => s.getTracks().every(t => t.readyState === 'ended'))), true, 'mode switch must release old microphone');
  assert.equal(await page.evaluate(() => current.pc.getSenders().filter(s => s.track).length), 0, 'playback-only must have no sending track');
  await page.waitForFunction(async () => {
    const reports = await current.pc.getStats();
    return [...reports.values()].some(r => r.type === 'inbound-rtp' && r.kind === 'audio' && r.totalAudioEnergy > .001);
  }, null, { timeout: 20000 });
  console.log('PASS native computer capture → Opus encode → browser playback without microphone permission');
  await page.getByRole('button', { name: '播放静音', exact: true }).click();
  await status(page, '播放已静音');
  assert.equal(await page.locator('#remote-audio').evaluate(a => a.muted), true);
  await page.getByRole('button', { name: '取消播放静音', exact: true }).click();
  await page.getByLabel('手机播放音量').fill('0.2');
  assert.equal(await page.locator('#remote-audio').evaluate(a => a.volume), .2);
  await page.evaluate(() => navigator.mediaDevices.dispatchEvent(new Event('devicechange')));
  await page.getByRole('button', { name: '启用播放', exact: true }).waitFor();
  assert.equal(await page.locator('#remote-audio').evaluate(a => a.paused), true);
  await page.getByRole('button', { name: '启用播放', exact: true }).click();
  await status(page, '正在播放');
  console.log('PASS playback volume, mute and device-change pause');
  await page.locator('#audio-settings > summary').click();
  const beforeQuality = await page.evaluate(() => window.microphoneRequests);
  for (const quality of ['music', 'weak', 'voice', 'balanced']) {
    await page.getByLabel('音质与网络').selectOption(quality);
    await status(page, '正在播放');
    assert.equal(await page.evaluate(async () => (await api('/api/status')).quality.id), quality);
    assert.equal(await page.evaluate(() => window.microphoneRequests), beforeQuality);
  }
  console.log('PASS all quality presets apply through renegotiation without microphone requests');

  await page.getByLabel('音质与网络').selectOption('ultra');
  await status(page, '正在播放');
  await page.waitForFunction(async () => {
    const reports = await current.pc.getStats();
    return [...reports.values()].some(r => r.type === 'inbound-rtp' && r.kind === 'audio' && r.totalAudioEnergy > .001);
  }, null, { timeout: 20000 });
  assert.equal(await page.evaluate(() => window.microphoneRequests), beforeQuality);
  assert.match(await page.locator('#quality-hint').textContent(), /384 kbps.*发送缓冲 20 ms.*自动/);
  assert.equal(await page.evaluate(() => current.pc.remoteDescription.sdp.includes('maxaveragebitrate=384000')), true);
  const ultraStatus = await page.evaluate(async () => await api('/api/status'));
  assert.equal(ultraStatus.quality.channels, 2);
  assert.equal(ultraStatus.quality.playback_only, true);
  assert.deepEqual(ultraStatus.buffers, {send_ms:20,play_ms:0});
  for (const mode of ['mic', 'duplex']) {
    assert.equal(await page.evaluate(async mode => (await fetch('/api/session', { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ type: 'offer', sdp: '', mode, quality: 'ultra' }) })).status, mode), 400);
  }
  assert.equal(await page.evaluate(async () => (await api('/api/status')).quality.id), 'ultra');
  console.log('PASS ultra stereo playback, independent short buffers and server rejection of microphone modes');

  await page.getByLabel('连接方式').selectOption('ws-pcm');
  await status(page, '正在播放');
  await page.waitForFunction(() => {
    if (!current?.gain) return false;
    if (!current.testAnalyser) {current.testAnalyser=current.context.createAnalyser();current.gain.disconnect();current.gain.connect(current.testAnalyser).connect(current.context.destination);}
    const data=new Float32Array(current.testAnalyser.fftSize);current.testAnalyser.getFloatTimeDomainData(data);
    return data.some(value=>Math.abs(value)>.005);
  }, null, {timeout:20000});
  const pcmID=await page.evaluate(()=>current.id);
  const audibleSamples=await page.evaluate(async()=>{
    let audible=0;
    for(let i=0;i<40;i++){
      await new Promise(resolve=>setTimeout(resolve,100));
      if(!current?.testAnalyser) continue;
      const data=new Float32Array(current.testAnalyser.fftSize);current.testAnalyser.getFloatTimeDomainData(data);
      if(data.some(value=>Math.abs(value)>.005)) audible++;
    }
    return audible;
  });
  assert.ok(audibleSamples>=30,`PCM capture/playout had too many silent gaps: ${audibleSamples}/40`);
  assert.equal(await page.evaluate(()=>current.id),pcmID,'steady PCM playback must not reconnect');
  assert.equal(await page.evaluate(() => window.microphoneRequests), beforeQuality);
  assert.equal(await page.evaluate(() => current.pc === undefined), true);
  assert.equal(await page.evaluate(async () => (await api('/api/status')).transport), 'ws-pcm');
  assert.equal(await page.getByLabel('音质与网络').isDisabled(), true);
  await page.getByRole('button',{name:'播放静音',exact:true}).click();
  assert.equal(await page.evaluate(() => current.gain.gain.value), 0);
  await page.getByRole('button',{name:'取消播放静音',exact:true}).click();
  await page.getByLabel('手机播放音量').fill('0.25');
  assert.equal(await page.evaluate(() => current.gain.gain.value), .25);
  const occupied=await other.evaluate(async()=>{try {await api('/api/pcm',{method:'POST',body:JSON.stringify({mode:'playback'})});return 200;} catch(error){return error.status;}});
  assert.equal(occupied,409);
  await page.getByLabel('电脑发送缓冲（ms）').fill('40');
  await page.getByLabel('电脑发送缓冲（ms）').dispatchEvent('change');
  await status(page,'正在播放');
  await page.getByLabel('手机播放缓冲').selectOption('80');
  await status(page,'正在播放');
  await page.waitForFunction(async()=>{const s=await api('/api/status');return s.buffers.send_ms===40 && s.buffers.play_ms===80 && s.playback.received_frames>0 && s.playback.target_ms===80;});
  await page.locator('#network-info').filter({hasText:/HTTP RTT.*手机队列/}).waitFor();
  const previousPCM=await page.evaluate(()=>current.id);
  await page.evaluate(()=>current.socket.close());
  await status(page,'正在重新连接');await status(page,'正在播放');
  assert.notEqual(await page.evaluate(()=>current.id),previousPCM);
  await page.evaluate(()=>navigator.mediaDevices.dispatchEvent(new Event('devicechange')));
  await status(page,'尚未连接');
  assert.equal(await page.evaluate(()=>current),null);
  await page.getByRole('button',{name:'开始传音'}).click();await status(page,'正在播放');
  await page.getByLabel('手机播放缓冲').selectOption('0');await status(page,'正在播放');
  await page.getByLabel('PCM 压缩').selectOption('deflate');await status(page,'正在播放');
  await page.waitForFunction(async()=>{const s=await api('/api/status');return s.pcm_compression==='deflate' && s.playback.received_frames>0;});
  assert.equal(await page.evaluate(()=>current.pendingDecode<10),true);
  await page.getByLabel('工作模式').selectOption('duplex');await status(page,'双向传音中');
  await page.waitForFunction(async()=>{const s=await api('/api/status');return s.receive.audio.samples>0 && s.send.audio.samples>0;});
  assert.equal(await page.evaluate(()=>current.pc===undefined && current.stream.getAudioTracks().length===1),true);
  await page.getByRole('button',{name:'静音',exact:true}).click();
  await page.locator('#receive-state').filter({hasText:'已静音'}).waitFor();
  assert.equal(await page.evaluate(()=>current.gain.gain.value>0),true);
  await page.getByRole('button',{name:'取消静音',exact:true}).click();
  await page.getByRole('button',{name:'播放静音',exact:true}).click();
  assert.equal(await page.evaluate(()=>current.stream.getAudioTracks().every(track=>track.enabled)),true);
  await page.getByRole('button',{name:'取消播放静音',exact:true}).click();
  await page.getByLabel('工作模式').selectOption('mic');await status(page,'正在传音');
  assert.equal(await page.evaluate(()=>current.player===undefined),true);
  await page.getByLabel('工作模式').selectOption('playback');await status(page,'正在播放');
  assert.equal(await page.evaluate(()=>current.stream===undefined),true);
  await page.getByLabel('连接方式').selectOption('webrtc');await status(page,'正在播放');
  console.log('PASS compressed WS microphone, playback and duplex; independent mute, mode changes and microphone release');
  console.log('PASS PCM AudioWorklet playback, independent buffers, metrics, mute, volume, exclusive session, reconnect and device-change stop');

  await page.getByLabel('工作模式').selectOption('duplex');
  await status(page, '双向传音中');
  assert.equal(await page.getByLabel('音质与网络').inputValue(), 'music');
  assert.equal(await page.locator('#quality option[value="ultra"]').isDisabled(), true);
  await page.getByRole('button', { name: '静音', exact: true }).click();
  await page.locator('#receive-state').filter({ hasText: '已静音' }).waitFor();
  assert.equal(await page.locator('#remote-audio').evaluate(a => a.paused || a.muted), false, 'microphone mute must not mute playback');
  await page.getByRole('button', { name: '取消静音', exact: true }).click();
  await page.getByRole('button', { name: '播放静音', exact: true }).click();
  await page.waitForFunction(() => current.stream.getAudioTracks().every(t => t.enabled), null, { timeout: 5000 });
  await page.getByRole('button', { name: '取消播放静音', exact: true }).click();
  console.log('PASS duplex and independent direction controls');
  await page.locator('#receive-state').filter({ hasText: '正在传音' }).waitFor();
  await page.locator('#send-state').filter({ hasText: '正在传音' }).waitFor();
  await mkdir(`${root}/artifacts`, { recursive: true });
  await page.screenshot({ path: `${root}/artifacts/duplex-client.png`, fullPage: true });

  loopback = pulse(['load-module', 'module-loopback', 'source=rmic_mic', 'sink=rmic_computer', 'latency_msec=20']);
  await page.locator('#send-state').filter({ hasText: '电脑采集已暂停' }).waitFor({ timeout: 12000 });
  await page.locator('#receive-state').filter({ hasText: '正在传音' }).waitFor();
  await page.getByRole('alert').filter({ hasText: '反馈路径' }).waitFor();
  pulse(['unload-module', loopback]); loopback = null;
  console.log('PASS dynamic feedback route pauses only computer-to-phone');

  await page.getByRole('button', { name: '断开连接', exact: true }).click();
  await status(page, '尚未连接');
  await other.getByRole('button', { name: '开始传音' }).click();
  await status(other, '正在传音');
  await other.getByRole('button', { name: '断开连接', exact: true }).click();
  console.log('PASS session release and another device reconnect');

  const deniedContext = await browser.newContext({ ignoreHTTPSErrors: true });
  await deniedContext.addInitScript(() => { navigator.mediaDevices.getUserMedia = async () => { throw new DOMException('Permission denied', 'NotAllowedError'); }; });
  const denied = await deniedContext.newPage(); denied.on('pageerror', error => errors.push(error.message));
  await denied.goto(url);
  await denied.getByRole('button', { name: '开始传音' }).click();
  await denied.getByRole('alert').filter({ hasText: '麦克风权限被拒绝' }).waitFor();
  assert.equal(await denied.getByRole('button', { name: '开始传音' }).isEnabled(), true);
  console.log('PASS microphone permission denial is recoverable');
  assert.deepEqual(errors, [], 'browser must not report uncaught errors');
} finally {
  clearInterval(feedTimer);
  producer?.kill('SIGTERM');
  if (loopback) pulse(['unload-module', loopback]);
  await browser?.close();
  app.kill('SIGTERM');
  await new Promise(resolve => { if (app.exitCode !== null || app.signalCode !== null) resolve(); else app.once('exit', resolve); });
  await rm(config, { recursive: true, force: true });
}
