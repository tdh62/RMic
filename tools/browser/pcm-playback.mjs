import assert from 'node:assert/strict';
import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

// Exercise the real AudioWorklet without a native audio server or Go binary.
const worklet = await readFile(new URL('../../web/static/pcm-player.js', import.meta.url));
const server = http.createServer((request, response) => {
  response.setHeader('Content-Type', request.url === '/pcm-player.js' ? 'text/javascript' : 'text/html');
  response.end(request.url === '/pcm-player.js' ? worklet : '<title>PCM playback check</title>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch({ headless: true, channel: 'chromium', args: ['--autoplay-policy=no-user-gesture-required'] });
  await Promise.all([44100, 48000].map(async rate => {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    const result = await page.evaluate(async rate => {
      const context = new AudioContext({ sampleRate: rate });
      await context.audioWorklet.addModule('/pcm-player.js');
      const player = new AudioWorkletNode(context, 'rmic-pcm', {
        numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [2], processorOptions: { playMS: 0 }
      });
      const analyser = context.createAnalyser();
      player.connect(analyser).connect(context.destination);
      let metrics = null, processorErrors = 0, queueErrors = 0, frames = 0;
      let audible = 0, checks = 0;
      player.onprocessorerror = () => processorErrors++;
      player.port.onmessage = event => {
        if (event.data.type === 'metrics') metrics = event.data;
        else queueErrors++;
      };
      await context.resume();
      const started = context.currentTime;
      let next = 0;
      const tick = setInterval(() => {
        const elapsed = context.currentTime - started;
        // A delayed delivery burst tests actual audio-thread rebuffering.
        if (elapsed >= 4 && elapsed < 4.8) return;
        while (next <= elapsed) {
          const data = new ArrayBuffer(3856), view = new DataView(data);
          for (let i = 0; i < 960; i++) {
            view.setInt16(16 + i * 4, Math.round(8000 * Math.sin(2 * Math.PI * 440 * (frames + i) / 48000)), true);
            view.setInt16(18 + i * 4, Math.round(8000 * Math.sin(2 * Math.PI * 880 * (frames + i) / 48000)), true);
          }
          player.port.postMessage(data, [data]);
          frames += 960; next += .02;
        }
        const values = new Float32Array(analyser.fftSize);
        analyser.getFloatTimeDomainData(values);
        if (elapsed > 1 && !(elapsed >= 4 && elapsed < 6)) {
          checks++;
          if (values.some(value => Math.abs(value) > .01)) audible++;
        }
      }, 20);
      try {
        const deadline=performance.now()+30000;
        while(context.currentTime-started<12) {
          if(performance.now()>deadline) throw new Error('AudioWorklet did not advance its audio clock');
          await new Promise(resolve=>setTimeout(resolve,100));
        }
        return { rate: context.sampleRate, metrics, processorErrors, queueErrors, audible, checks };
      } finally {
        clearInterval(tick); player.disconnect(); analyser.disconnect(); await context.close();
      }
    }, rate);
    assert.equal(result.rate, rate);
    assert.equal(result.processorErrors, 0);
    assert.equal(result.queueErrors, 0);
    assert.ok(result.metrics, 'audio thread produced no telemetry');
    assert.ok(result.metrics.underruns >= 1, 'delivery stall did not exercise rebuffering');
    assert.ok(result.metrics.queued_ms < 1500, 'playback queue grew unexpectedly');
    assert.ok(result.audible > result.checks * .85, `too many silent checks: ${JSON.stringify(result)}`);
    assert.deepEqual(errors, []);
    console.log(`PASS real browser AudioWorklet: ${rate} Hz, stereo tone, delivery stall and resumed playback`);
    await page.close();
  }));
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
