import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {deflateSync,inflateSync} from 'node:zlib';
import {chromium} from 'playwright';
const source=await readFile(new URL('../../web/static/pcm-wire.js',import.meta.url));
const server=createServer((req,res)=>{res.setHeader('Content-Type',req.url==='/pcm-wire.js'?'text/javascript':'text/html');res.end(req.url==='/pcm-wire.js'?source:'<title>PCM codec tests</title>');});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
let browser;
function packet(channels,raw){
 const compressed=deflateSync(raw),out=Buffer.alloc(24+compressed.length);
 out.write('RMPC');out.writeUInt16LE(2,4);out.writeUInt16LE(channels,6);out.writeUInt32LE(960,16);out.writeUInt32LE(1,20);compressed.copy(out,24);return [...out];
}
try {
 browser=await chromium.launch({channel:'chromium',headless:true});
 const page=await browser.newPage();await page.goto(`http://127.0.0.1:${server.address().port}/`);
 for(const channels of [2,6,8]) {
  const raw=Buffer.alloc(960*channels*2);for(let i=0;i<raw.length/2;i++)raw.writeInt16LE((i%channels+1)*1000,i*2);
  const decoded=await page.evaluate(async({channels,packet})=>{
   const worker=new Worker('/pcm-wire.js');
   try {
    worker.postMessage({type:'configure',channels,compression:'deflate'});
    const data=new Uint8Array(packet).buffer;
    const result=new Promise((resolve,reject)=>{worker.onmessage=event=>resolve(event.data);worker.onerror=reject;});
    worker.postMessage({type:'decode',packet:data},[data]);
    const message=await result;
    if(message.type==='error')throw new Error(message.message);
    return {frames:message.frames,raw:Array.from(new Uint8Array(message.packet,16))};
   }finally{worker.terminate();}
  },{channels,packet:packet(channels,raw)});
  assert.equal(decoded.frames,960);assert.deepEqual(Buffer.from(decoded.raw),raw);
  console.log(`PASS browser DEFLATE decode: ${channels} independent PCM channels, lossless bytes`);
 }
 const pcm=Buffer.alloc(3840);for(let i=0;i<pcm.length/2;i++)pcm.writeInt16LE(i%2?2000:-1000,i*2);
 const encoded=await page.evaluate(async data=>{
  const worker=new Worker('/pcm-wire.js');
  try {
   worker.postMessage({type:'configure',channels:2,compression:'deflate'});
   const result=new Promise((resolve,reject)=>{worker.onmessage=event=>resolve(event.data);worker.onerror=reject;});
   const pcm=new Uint8Array(data).buffer;worker.postMessage({type:'encode',pcm,offset:960},[pcm]);
   const message=await result;if(message.type==='error')throw new Error(message.message);
   return [...new Uint8Array(message.packet)];
  }finally{worker.terminate();}
 },[...pcm]);
 const result=Buffer.from(encoded);assert.equal(result.readUInt32LE(20),1);assert.equal(result.readBigUInt64LE(8),960n);assert.deepEqual(inflateSync(result.subarray(24)),pcm);
 console.log('PASS browser microphone compression interoperates with zlib and preserves stereo samples');
 for(const malformed of ['bomb','wrong sequence','truncated']) {
  let data=Buffer.from(packet(2,malformed==='bomb'?Buffer.alloc(1024*1024):pcm));
  if(malformed==='wrong sequence')data.writeBigUInt64LE(960n,8);
  if(malformed==='truncated')data=data.subarray(0,data.length-2);
  const type=await page.evaluate(async data=>{
   const worker=new Worker('/pcm-wire.js');try{
    worker.postMessage({type:'configure',channels:2,compression:'deflate'});
    const result=new Promise(resolve=>worker.onmessage=event=>resolve(event.data.type));
    const packet=new Uint8Array(data).buffer;worker.postMessage({type:'decode',packet},[packet]);return await result;
   }finally{worker.terminate();}
  },[...data]);
  assert.equal(type,'error',malformed+' accepted');
 }
 console.log('PASS browser decoder rejects decompression bombs, discontinuous sample positions and truncated streams');
}finally{await browser?.close();await new Promise(resolve=>server.close(resolve));}
