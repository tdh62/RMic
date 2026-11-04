'use strict';
function pcmVolume(c) {
  if (c?.gain) c.gain.gain.value = playbackMuted || c.playbackPaused ? 0 : Number($('playback-volume').value);
}
async function connectPCM(c, attempt) {
  if (!window.AudioWorkletNode || !window.Worker) throw Object.assign(new Error('此浏览器不支持 PCM 音频处理，请选择 WebRTC。'),{name:'NotSupportedError'});
  c.context ||= new AudioContext({latencyHint:'interactive'});
  const channels=availableCaptureChannels;
  const compression=$('pcm-compression').value;
  if(compression==='deflate') {
    try {new DecompressionStream('deflate');if(microphoneMode(c.mode)) new CompressionStream('deflate');}
    catch {throw Object.assign(new Error('此浏览器不支持 PCM 压缩，请选择原始 PCM。'),{name:'NotSupportedError'});}
  }
  const outputChannels=$('pcm-output').value==='stereo'?2:channels;
  if(playbackMode(c.mode) && c.context.destination.maxChannelCount<outputChannels) throw Object.assign(new Error(`此播放设备最多 ${c.context.destination.maxChannelCount} 声道；请选择立体声下混或更换输出设备。`),{name:'NotSupportedError'});
  c.codec=new Worker('/pcm-wire.js');
  c.codec.postMessage({type:'configure',channels,compression});
  c.pendingDecode=0;c.pendingEncode=0;c.receivedFrames=0;c.microphoneFrames=0;
  c.codec.onmessage=event=>{
    if(current!==c)return;
    const message=event.data;
    if(message.type==='error'){retry(new Error(message.message));return;}
    if(message.type==='decoded') {
      c.pendingDecode--;c.receivedFrames=message.frames;
      c.player.port.postMessage(message.packet,[message.packet]);
    } else if(message.type==='encoded') {
      c.pendingEncode--;
      if(c.socket?.readyState!==WebSocket.OPEN)return;
      if(c.socket.bufferedAmount>192000){retry(new Error('麦克风发送积压，请检查网络。'));return;}
      c.socket.send(message.packet);
    }
  };
  c.codec.onerror=()=>{if(current===c)retry(Object.assign(new Error('PCM 编解码器异常。'),{name:'NotSupportedError'}));};
  if(playbackMode(c.mode)) {
    await c.context.audioWorklet.addModule('/pcm-player.js');if(current!==c)return;
    c.context.destination.channelCount=outputChannels;
    c.player=new AudioWorkletNode(c.context,'rmic-pcm',{numberOfInputs:0,numberOfOutputs:1,outputChannelCount:[outputChannels],processorOptions:{playMS:selectedBuffers().play_ms,channels,outputChannels}});
    c.player.onprocessorerror=()=>{if(current===c)retry(Object.assign(new Error('PCM 音频处理器异常。'),{name:'NotSupportedError'}));};
    c.gain=c.context.createGain();pcmVolume(c);c.player.connect(c.gain).connect(c.context.destination);
    c.player.port.onmessage=event=>{
      if(current!==c)return;
      if(event.data.type==='error'){retry(new Error(event.data.message));return;}
      c.playbackMetrics=event.data;
    };
  }
  await c.context.resume();if(current!==c)return;
  const result=await api('/api/pcm',{method:'POST',headers:sessionHeaders(c),body:JSON.stringify({mode:c.mode,buffers:selectedBuffers(),channels,compression})});
  if(current!==c || attempt!==generation || !wanted){deleteSession(result.id);return;}
  c.id=result.id;resumeID=result.id;
  const url=new URL('/api/pcm',location.href);url.protocol='wss:';url.searchParams.set('ticket',result.ticket);
  c.socket=new WebSocket(url);c.socket.binaryType='arraybuffer';
  c.socket.onmessage=async event=>{
    if(current!==c)return;
    try {
      if(typeof event.data==='string') {
        const format=JSON.parse(event.data);
        const layouts={2:['FL','FR'],6:['FL','FR','FC','LFE','SL','SR'],8:['FL','FR','FC','LFE','BL','BR','SL','SR']};
        if(c.ready || format.type!=='format' || format.version!==2 || format.codec!=='pcm_s16le' || format.sample_rate!==48000 || format.channels!==channels || format.frame_samples!==960 || format.compression!==compression || format.microphone_channels!==2 || format.mode!==c.mode || JSON.stringify(format.layout)!==JSON.stringify(layouts[channels])) throw new Error('电脑返回了不支持的 PCM 格式。');
        c.ready=true;manualConnection=false;clearTimeout(c.connectTimeout);wake(c);
        c.feedbackTimer=setInterval(()=>{
          if(current!==c || c.socket.readyState!==WebSocket.OPEN)return;
          const m=c.playbackMetrics || {queued_ms:0,target_ms:0,underruns:0};
          c.socket.send(JSON.stringify({type:'metrics',queued_ms:m.queued_ms,target_ms:m.target_ms,underruns:m.underruns,received_frames:c.receivedFrames,rtt_ms:c.rttMS || 0}));
        },200);
        if(c.stream) {
          await c.context.audioWorklet.addModule('/pcm-capture.js');if(current!==c)return;
          if(c.context.sampleRate!==48000)throw Object.assign(new Error('此浏览器无法提供 48 kHz 麦克风 PCM。'),{name:'NotSupportedError'});
          c.capture=new AudioWorkletNode(c.context,'rmic-capture',{numberOfInputs:1,numberOfOutputs:1,outputChannelCount:[1]});
          c.capture.onprocessorerror=()=>{if(current===c)retry(new Error('麦克风 PCM 处理器异常。'));};
          c.captureSource=c.context.createMediaStreamSource(c.stream);
          c.captureSource.connect(c.capture).connect(c.context.destination);
          c.capture.port.onmessage=event=>{
            if(current!==c || c.socket.readyState!==WebSocket.OPEN)return;
            if(++c.pendingEncode>50){retry(new Error('麦克风编码积压。'));return;}
            const pcm=event.data;
            c.codec.postMessage({type:'encode',pcm,offset:c.microphoneFrames},[pcm]);c.microphoneFrames+=960;
          };
          $('mute').disabled=c.micPaused;
        }
        return;
      }
      if(!c.ready || !playbackMode(c.mode))throw new Error('未协商的 PCM 音频数据。');
      if(++c.pendingDecode>50)throw new Error('手机 PCM 解码积压。');
      c.codec.postMessage({type:'decode',packet:event.data},[event.data]);
    } catch(error) {if(current===c)retry(error);}
  };
  c.socket.onerror=()=>{if(current===c)retry(new Error('WebSocket 连接失败，请检查证书和网络。'));};
  c.socket.onclose=()=>{if(current===c)retry(new Error('PCM 音频连接已关闭，正在重新连接。'));};
}
