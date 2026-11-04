import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../../web/static/pcm-player.js',import.meta.url),'utf8');
function player(rate,playMS=0,channels=2,outputChannels=channels){
 let Processor;const messages=[];
 const context=vm.createContext({sampleRate:rate,AudioWorkletProcessor:class{constructor(){this.port={postMessage:m=>messages.push(m)};}},registerProcessor:(name,p)=>{Processor=p;},Float32Array,DataView});
 vm.runInContext(source,context);
 return {node:new Processor({processorOptions:{playMS,channels,outputChannels}}),messages};
}
function packet(){const data=new ArrayBuffer(3856),view=new DataView(data);for(let i=16;i<3856;i+=4){view.setInt16(i,8192,true);view.setInt16(i+2,-16384,true);}return data;}
for(const rate of [44100,48000]){
 const {node,messages}=player(rate);let feedAt=0,audible=0;
 for(let position=0;position<rate*35;position+=128){
  const time=position/rate;
  while(feedAt<=time){node.port.onmessage({data:packet()});feedAt+=.02;}
  const left=new Float32Array(128),right=new Float32Array(128);
  node.process([],[[left,right]]);
  for(let i=0;i<left.length;i++)if(left[i]!==0){audible++;assert.ok(Math.abs(left[i]-.25)<.00001);assert.ok(Math.abs(right[i]+.5)<.00001);assert.ok(time>=.10,'played before initial target');}
 }
 assert.ok(audible>rate*33,'long playback became silent');
 assert.equal(node.underruns,0,'sample-rate conversion or clock correction underrun');
 assert.ok(node.size/48<200,'queue grew without bound');
 assert.equal(node.targetMS,80,'automatic target did not decrease after stable playback');
 assert.equal(messages.some(m=>m.type==='error'),false);
 for(let i=0;i<rate/128;i++)node.process([],[[new Float32Array(128),new Float32Array(128)]]);
 assert.equal(node.underruns,1);assert.equal(node.targetMS,120,'automatic target did not increase after underrun');
 console.log(`PASS PCM worklet: ${rate} Hz output, stereo, 35s steady playback, bounded queue and adaptive targets`);
}
const {node}=player(48000,40);node.port.onmessage({data:packet()});node.process([],[[new Float32Array(128),new Float32Array(128)]]);assert.equal(node.primed,false);node.port.onmessage({data:packet()});node.process([],[[new Float32Array(128),new Float32Array(128)]]);assert.equal(node.primed,true);assert.equal(node.targetMS,40);
console.log('PASS fixed PCM receiver target stays independent of sender buffering');


// Simulate source and destination clocks independently. This exercises hours
// of sample positions in seconds without claiming a hardware soak test.
function simulate(node, seconds, rate, clockPPM = 0, stall = null) {
 const output = [[new Float32Array(128), new Float32Array(128)]];
 const data = packet();
 let next = 0, audible = 0, maxQueued = 0, maxStep = 0;
 for (let position = 0; position < rate * seconds; position += 128) {
  const now = position / rate;
  const before = node.correction;
  if (!stall || now < stall[0] || now >= stall[1]) {
   while (next <= now) {
    node.port.onmessage({data});
    next += .02 / (1 + clockPPM / 1e6);
   }
  }
  output[0][0].fill(0); output[0][1].fill(0);
  node.process([], output);
  if (output[0][0].some(v => Math.abs(v) > .01)) audible++;
  maxQueued = Math.max(maxQueued, node.size / 48);
  maxStep = Math.max(maxStep, Math.abs(node.correction - before));
 }
 return {audible, maxQueued, maxStep};
}
for (const rate of [44100, 48000]) {
 for (const ppm of [-200, 200]) {
  const {node, messages} = player(rate);
  const result = simulate(node, 600, rate, ppm);
  assert.equal(node.underruns, 0, `clock drift ${ppm} ppm caused an underrun`);
  assert.ok(node.size / 48 < 120, `clock drift accumulated latency: ${node.size / 48}`);
  assert.equal(node.targetMS, 60);
  assert.ok(result.audible > rate * 595 / 128, 'long playback became silent');
  assert.ok(result.maxStep <= 128 / rate * .002 + 1e-12, 'clock correction changed abruptly');
  assert.equal(messages.some(m => m.type === 'error'), false);
  console.log(`PASS 10-minute PCM simulation: ${rate} Hz, ${ppm} ppm clock drift, no underruns or growing backlog`);
 }
}
{
 const {node, messages} = player(48000);
 const result = simulate(node, 180, 48000, 0, [5, 5.8]);
 assert.equal(node.underruns, 1, 'network stall did not cause exactly one rebuffer');
 assert.ok(result.maxQueued > 700, 'test did not reproduce a delayed TCP burst');
 assert.ok(node.size / 48 < 150, `stale backlog failed to recover: ${node.size / 48} ms`);
 assert.equal(node.targetMS, 60);
 assert.equal(messages.some(m => m.type === 'error'), false);
 console.log('PASS 800 ms delivery stall: one rebuffer, bounded burst, automatic target and backlog recover');
}
{
 const {node} = player(48000);
 simulate(node, 60, 48000, 0, [0, 60]);
 assert.equal(node.targetMS, 120, 'silence before priming counted as stable playback');
 assert.equal(node.stable, 0);
 console.log('PASS waiting for initial audio does not decrease the adaptive target');
}

for(const channels of [6,8]) {
 for(const outputChannels of [channels,2]) {
  const {node}=player(48000,40,channels,outputChannels);
  const data=new ArrayBuffer(16+960*channels*2),view=new DataView(data);
  for(let frame=0;frame<960;frame++)for(let channel=0;channel<channels;channel++)view.setInt16(16+(frame*channels+channel)*2,(channel+1)*1000,true);
  node.port.onmessage({data});node.port.onmessage({data});
  const outputs=Array.from({length:outputChannels},()=>new Float32Array(128));
  node.process([],[outputs]);
  if(outputChannels===channels) {
   for(let channel=0;channel<channels;channel++)assert.ok(Math.abs(outputs[channel][0]-(channel+1)*1000/32768)<1e-7,'multichannel playback order changed');
  } else {
   let left=1000+.7071*(3000+5000),right=2000+.7071*(3000+6000);
   if(channels===8){left+=.7071*7000;right+=.7071*8000;}
   const divisor=channels===8?3.1213:2.4142;
   assert.ok(Math.abs(outputs[0][0]-left/divisor/32768)<1e-7);
   assert.ok(Math.abs(outputs[1][0]-right/divisor/32768)<1e-7);
  }
  console.log(`PASS ${channels}-channel PCM: ${outputChannels}-channel output, explicit mapping and downmix`);
 }
}
{
 let Processor;const packets=[];
 const context=vm.createContext({AudioWorkletProcessor:class{constructor(){this.port={postMessage:packet=>packets.push(packet)};}},registerProcessor:(name,p)=>{Processor=p;}});
 vm.runInContext(await readFile(new URL('../../web/static/pcm-capture.js',import.meta.url),'utf8'),context);
 const capture=new Processor();
 for(let i=0;i<15;i++)capture.process([[new Float32Array(128).fill(.25),new Float32Array(128).fill(-.5)]]);
 assert.equal(packets.length,2);
 for(const packet of packets){const view=new DataView(packet);assert.equal(view.byteLength,3840);for(let i=0;i<960;i++){assert.equal(view.getInt16(i*4,true),8192);assert.equal(view.getInt16(i*4+2,true),-16384);}}
 console.log('PASS microphone worklet: exact 20ms frame aggregation, channel separation and zero monitoring output');
}
