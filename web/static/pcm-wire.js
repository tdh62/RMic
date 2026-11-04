'use strict';
let channels = 2, compression = 'none', received = 0n;
let decoding = Promise.resolve(), encoding = Promise.resolve();
let queued = 0;
async function transform(data, compress, limit) {
  const stream = new Blob([data]).stream().pipeThrough(compress ? new CompressionStream('deflate') : new DecompressionStream('deflate'));
  const reader = stream.getReader();
  const chunks = []; let size = 0;
  try {
    while (true) {
      const {done, value} = await reader.read(); if (done) break;
      size += value.byteLength;
      if (size > limit) throw new Error('PCM 压缩数据超出长度限制。');
      chunks.push(value);
    }
  } finally { await reader.cancel().catch(() => {}); }
  const out = new Uint8Array(size); let offset = 0;
  for (const chunk of chunks) { out.set(chunk, offset); offset += chunk.byteLength; }
  return out;
}
function fail(error) { postMessage({type:'error',message:error.message}); }
onmessage = event => {
  const message = event.data;
  if (message.type === 'configure') {
    channels = message.channels; compression = message.compression;
    if (![2,6,8].includes(channels) || !['none','deflate'].includes(compression)) fail(new Error('不支持的 PCM 格式。'));
    return;
  }
  if (++queued > 100) { fail(new Error('PCM 编解码队列积压，请重新连接。')); return; }
  const run = async () => {
    try {
      if (message.type === 'decode') {
        const packet = message.packet, view = new DataView(packet), expected = 960 * channels * 2;
        if (packet.byteLength < 24 || packet.byteLength > expected + 24 || view.getUint32(0,false)!==0x524d5043 || view.getUint16(4,true)!==2 || view.getUint16(6,true)!==channels || view.getBigUint64(8,true)!==received || view.getUint32(16,true)!==960) throw new Error('PCM 格式或采样序号错误。');
        const flags = view.getUint32(20,true);
        let raw = new Uint8Array(packet,24);
        if (flags === 1 && compression === 'deflate') raw = await transform(raw,false,expected);
        else if (flags !== 0) throw new Error('PCM 压缩标志错误。');
        if (raw.byteLength !== expected) throw new Error('PCM 解码长度错误。');
        const normalized = new ArrayBuffer(16 + expected);
        new Uint8Array(normalized).set(new Uint8Array(packet,0,16));
        new Uint8Array(normalized,16).set(raw);
        received += 960n;
        postMessage({type:'decoded',packet:normalized,frames:Number(received)},[normalized]);
      } else if (message.type === 'encode') {
        const raw = new Uint8Array(message.pcm);
        if (raw.byteLength!==3840 || !Number.isSafeInteger(message.offset) || message.offset<0) throw new Error('麦克风 PCM 格式错误。');
        let payload = raw, flags = 0;
        if (compression==='deflate') {
          const compressed = await transform(raw,true,raw.byteLength+1024);
          if (compressed.byteLength<raw.byteLength) {payload=compressed;flags=1;}
        }
        const packet = new ArrayBuffer(24+payload.byteLength), view = new DataView(packet);
        view.setUint32(0,0x524d5043,false);view.setUint16(4,2,true);view.setUint16(6,2,true);
        view.setBigUint64(8,BigInt(message.offset),true);view.setUint32(16,960,true);view.setUint32(20,flags,true);
        new Uint8Array(packet,24).set(payload);
        postMessage({type:'encoded',packet},[packet]);
      }
    } catch(error) { fail(error); }
    finally { queued--; }
  };
  if (message.type==='decode') decoding=decoding.then(run);
  else encoding=encoding.then(run);
};
