import assert from 'node:assert/strict';
import {spawn,execFileSync} from 'node:child_process';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {chromium} from 'playwright';
const root=fileURLToPath(new URL('../../',import.meta.url));
const pulse=args=>execFileSync('pactl',args,{encoding:'utf8',env:{...process.env,LC_ALL:'C'}}).trim();
let browser;
try {
 browser=await chromium.launch({channel:'chromium',headless:true});
 for(const channels of [6,8]) {
  const name=`rmic_browser_${channels}`,config=await mkdtemp(`${tmpdir()}/rmic-multi-`);
  const layout=channels===6?'front-left,front-right,front-center,lfe,side-left,side-right':'front-left,front-right,front-center,lfe,rear-left,rear-right,side-left,side-right';
  const module=pulse(['load-module','module-null-sink',`sink_name=${name}`,`sink_properties=device.description=${name}`,`channels=${channels}`,`channel_map=${layout}`,'rate=48000']);
  let app,producer,timer,output='';
  try {
   const devices=execFileSync(`${root}/bin/rmic`,['--list-devices'],{encoding:'utf8'});
   const source=devices.split('\n').map(line=>line.trim()).find(line=>line.includes(name));
   assert.ok(source,'native multichannel monitor missing');
   app=spawn(`${root}/bin/rmic`,['--headless','--mode=playback','--virtual=off',`--capture=${source}`,`--capture-channels=${channels}`,'--ip=127.0.0.1','--listen=:0','--udp-port=0',`--config-dir=${config}`]);
   const url=await new Promise((resolve,reject)=>{
    const timeout=setTimeout(()=>reject(new Error(output)),10000);
    app.stdout.on('data',chunk=>{output+=chunk;const match=output.match(/https:\/\/127\.0\.0\.1:\d+\/#token=[a-f0-9]+/);if(match){clearTimeout(timeout);resolve(match[0]);}});
    app.stderr.on('data',chunk=>{output+=chunk;});app.on('error',reject);app.on('exit',()=>{clearTimeout(timeout);reject(new Error(output));});
   });
   producer=spawn('pacat',['--playback','--raw','--format=s16le','--rate=48000',`--channels=${channels}`,`--channel-map=${layout}`,`--device=${name}`]);producer.stdin.on('error',()=>{});
   let position=0;
   timer=setInterval(()=>{
    if(producer.stdin.destroyed || producer.stdin.writableNeedDrain)return;
    const frame=Buffer.alloc(960*channels*2);
    for(let i=0;i<960;i++)for(let channel=0;channel<channels;channel++)frame.writeInt16LE(Math.round(8000*Math.sin(2*Math.PI*(220+channel*110)*(position+i)/48000)),(i*channels+channel)*2);
    position+=960;producer.stdin.write(frame);
   },20);
   const context=await browser.newContext({ignoreHTTPSErrors:true});
   const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(url);
   await page.waitForFunction(()=>availableCaptureChannels>2);
   await page.locator('#audio-settings > summary').click();await page.getByLabel('连接方式').selectOption('ws-pcm');
   await page.getByRole('button',{name:'开始传音'}).click();
   await page.getByRole('alert').filter({hasText:'请选择立体声下混'}).waitFor();
   assert.equal(await page.evaluate(async()=>(await api('/api/status')).state),'waiting','unsupported output must not start a session');
   await page.getByLabel('PCM 播放声道').selectOption('stereo');await page.getByLabel('PCM 压缩').selectOption('deflate');
   await page.getByRole('button',{name:'开始传音'}).click();await page.locator('#status').filter({hasText:/^正在播放$/}).waitFor();
   await page.waitForFunction(()=>{
    if(!current?.gain)return false;
    if(!current.probe){current.probe=current.context.createAnalyser();current.gain.disconnect();current.gain.connect(current.probe).connect(current.context.destination);}
    const data=new Float32Array(current.probe.fftSize);current.probe.getFloatTimeDomainData(data);return data.some(v=>Math.abs(v)>.005);
   },null,{timeout:15000});
   const status=await page.evaluate(async()=>api('/api/status'));
   assert.equal(status.capture_channels,channels);assert.equal(status.pcm_compression,'deflate');assert.equal(status.receive.state,'disabled');
   assert.equal(await page.evaluate(()=>current.player.parameters.size),0);
   assert.deepEqual(errors,[]);
   await page.getByRole('button',{name:'断开连接',exact:true}).click();await context.close();
   console.log(`PASS native ${channels}-channel capture → compressed WSS → browser decode and explicit stereo downmix; unsupported output rejected`);
  } finally {
   clearInterval(timer);producer?.kill('SIGTERM');app?.kill('SIGTERM');
   if(app && app.exitCode===null && app.signalCode===null)await new Promise(resolve=>app.once('exit',resolve));
   pulse(['unload-module',module]);await rm(config,{recursive:true,force:true});
  }
 }
}finally{await browser?.close();}
