'use strict';
const $ = id => document.getElementById(id);
const remote = $('remote-audio');
remote.volume = 0.35;
const fragmentToken = new URLSearchParams(location.hash.slice(1)).get('token') || '';
const bootstrapToken = /^[a-f0-9]{48}$/.test(fragmentToken) ? fragmentToken : '';
let token = '', deviceID = '', pairTimer = null, pairGeneration = 0;
for (const read of [() => localStorage.getItem('rmic-device-auth'), () => sessionStorage.getItem('rmic-device-auth')]) {
  try {
    const saved = JSON.parse(read() || 'null');
    if (saved?.version === 1 && /^[a-f0-9]{48}$/.test(saved.token)) { token = saved.token; deviceID = saved.device_id || ''; break; }
  } catch (_) { /* Try session storage when persistent storage is unavailable. */ }
}
if (!token) {
  token = bootstrapToken;
  try { token ||= sessionStorage.getItem('rmic-token') || ''; } catch (_) {}
}
history.replaceState(null, '', location.pathname);
$('pairing').hidden = !!token;
$('device-name').value=/Android/i.test(navigator.userAgent)?'Android 手机':/iPhone|iPad/i.test(navigator.userAgent)?'iPhone / iPad':`${navigator.platform || '移动设备'} 浏览器`;
let current = null, wanted = false, muted = false, playbackMuted = false, generation = 0, manualConnection = false;
let retryTimer = null, retries = 0, polling = false, resumeID = '', capabilityLoaded = false;
let connectQueue = Promise.resolve(), observedInstance = '', observedRevision = 0;
$('trust-certificate').onclick = () => {
  stop();
  const url = new URL('/', location.origin);
  url.searchParams.set('rmic-trust', Date.now().toString());
  // A top-level navigation lets the browser show its certificate interstitial.
  // The service worker deliberately leaves this request to the browser.
  location.assign(url.href);
};
const shortcutControls = new RmicShortcuts(api);
let capabilities, availableCaptureChannels=2, availableModes = ['mic', 'playback', 'duplex'];
let profiles = [
  { id: 'voice', bitrate: 48000, channels: 1, frame_ms: 20, buffer_ms: 40 },
  { id: 'balanced', bitrate: 96000, channels: 2, frame_ms: 20, buffer_ms: 40 },
  { id: 'music', bitrate: 160000, channels: 2, frame_ms: 20, buffer_ms: 60 },
  { id: 'weak', bitrate: 24000, channels: 1, frame_ms: 40, buffer_ms: 120 },
  { id: 'ultra', bitrate: 384000, channels: 2, frame_ms: 20, buffer_ms: 120, playback_only: true }
];
const selectedBuffers = () => ({ send_ms: Number($('send-buffer').value), play_ms: Number($('play-buffer').value) });
const selectedQuality = () => profiles.find(p => p.id === $('quality').value);
function renderQuality() {
  let profile = selectedQuality(), notice = '';
  const playback = $('mode').value === 'playback';
  for (const option of $('quality').options) option.disabled = !!profiles.find(p => p.id === option.value)?.playback_only && !playback;
  if (profile.playback_only && !playback) {
    $('quality').value = 'music'; profile = selectedQuality();
    notice = 'Ultra 仅用于电脑 → 手机播放，已切换为高音质。';
  }
  const ws = $('connection').value === 'ws-pcm';
  $('quality').disabled = ws;
  const buffering = selectedBuffers();
  const bufferText = `电脑发送缓冲 ${buffering.send_ms} ms · 手机播放缓冲 ${buffering.play_ms || '自动'}${buffering.play_ms ? ' ms' : ''}。`;
  const extra = profile.playback_only ? '仅电脑 → 手机 · 48 kHz · 手机播放缓冲由浏览器决定。' : '';
  const wsFormat=playbackMode($('mode').value)?`下行 ${availableCaptureChannels} 声道 · 原始数据 ${(48000*availableCaptureChannels*16/1e6).toFixed(3)} Mbps`:'仅麦克风上行';
  $('quality-hint').textContent = ws ? `PCM 16 位 / 48 kHz · ${wsFormat} · 上行麦克风为双声道 · ${$('pcm-compression').value==='deflate'?'DEFLATE 无损压缩（压缩率随音源变化）':'原始 PCM'}。${bufferText}音质预设仅用于 WebRTC。` : `${notice}${profile.bitrate / 1000} kbps / 方向 · ${profile.channels === 1 ? '单声道' : '双声道'} · 电脑编码帧 ${profile.frame_ms} ms · ${bufferText}${extra}切换设置会重新连接。`;
  if(!ws && availableCaptureChannels>2 && playbackMode($('mode').value)) $('quality-hint').textContent+=` 电脑 ${availableCaptureChannels} 声道采集将下混为立体声后进行 Opus 编码。`;
  if (current?.quality.id === profile.id && current.playoutNote) $('quality-hint').textContent += ` ${current.playoutNote}`;
}
const microphoneMode = mode => mode === 'mic' || mode === 'duplex';
const playbackMode = mode => mode === 'playback' || mode === 'duplex';
function showError(text) { $('error').textContent = text; $('error').hidden = false; }
function state(title, detail, kind = '') {
  $('status').textContent = title;
  $('detail').textContent = detail;
  $('dot').className = `dot ${kind}`;
}
async function api(path, options = {}) {
  const abort = new AbortController();
  const cancelled = () => abort.abort(options.signal.reason);
  if (options.signal?.aborted) cancelled();
  else options.signal?.addEventListener('abort', cancelled, { once: true });
  const timeout = setTimeout(() => abort.abort(new DOMException('电脑响应超时', 'TimeoutError')), 12000);
  try {
    const response = await fetch(path, { ...options, headers: { 'Authorization': `Bearer ${token}`, 'Content-Type': 'application/json', ...options.headers }, signal: abort.signal });
    if (!response.ok) {
      const error = new Error((await response.text()).trim());
      error.status = response.status;
      error.code = response.headers.get('X-RMic-Error') || '';
      throw error;
    }
    return response.status === 204 ? null : await response.json();
  } catch (error) {
    // Fetch cannot distinguish a rejected certificate from other network errors.
    if (error.name === 'TypeError' && !abort.signal.aborted) $('certificate-help').open = true;
    throw error;
  } finally {
    clearTimeout(timeout);
    options.signal?.removeEventListener('abort', cancelled);
  }
}
function resetPairRequest() {
 pairGeneration++; clearTimeout(pairTimer);
 $('request-pair').disabled=false;$('remember-device').disabled=false;$('pair-code').hidden=true;
}
function clearAuthorization(){
 shortcutControls.update(null, '');
 resetPairRequest();token='';deviceID='';
 try{localStorage.removeItem('rmic-device-auth');}catch(_){}
 try{sessionStorage.removeItem('rmic-device-auth');sessionStorage.removeItem('rmic-token');}catch(_){}
 $('authorization').hidden=true;$('pairing').hidden=false;
}
function acceptAuthorization(result){
 resetPairRequest();token=result.token;deviceID=result.device_id;
 const saved=JSON.stringify({version:1,token,device_id:deviceID});let remembered=false;
 try{sessionStorage.setItem('rmic-device-auth',saved);sessionStorage.removeItem('rmic-token');}catch(_){}
 try{if(result.remember&&$('remember-device').checked){localStorage.setItem('rmic-device-auth',saved);remembered=true;}else{localStorage.removeItem('rmic-device-auth');}}catch(_){}
 $('pairing').hidden=true;$('authorization').hidden=false;$('error').hidden=true;
 $('auth-state').textContent=remembered?'电脑已许可，已记住设备；下次打开此地址可直接使用。':'电脑已许可；本次浏览器会话可使用。';
}
async function loadCapabilities(recovering = false, signal) {
  if (!token) return;
  const requestGeneration = generation;
  let status;
  try{status=await api('/api/status', { signal });}catch(error){
   if(error.status===401&&!capabilityLoaded&&bootstrapToken&&token!==bootstrapToken){clearAuthorization();token=bootstrapToken;status=await api('/api/status');}else{throw error;}
  }
  if (requestGeneration !== generation) return;
  shortcutControls.update(status.shortcuts, current?.id || '');
  if(!deviceID&&$('remember-device').checked){
   const result=await api('/api/pair/remember',{method:'POST',body:JSON.stringify({name:$('device-name').value})});acceptAuthorization(result);
  }else{
   if(!deviceID){try{sessionStorage.setItem('rmic-token',token);}catch(_){}}
   $('pairing').hidden=true;$('authorization').hidden=false;$('auth-state').textContent='此设备已获电脑许可。';
  }
  if (requestGeneration !== generation) return;
  if (recovering && !manualConnection && status.state === 'waiting' && status.error === '电脑端已断开连接') {
    throw Object.assign(new Error('电脑端已断开连接，点击开始可以重新连接。'), { status: 409, code: 'computer_disconnected' });
  }
  const previousMode = $('mode').value;
  availableCaptureChannels=status.capture_channels || 2;
  availableModes = status.modes;
  for (const option of $('mode').options) {
    option.disabled = !status.modes.includes(option.value);
    option.title = option.disabled ? '请在电脑配置并允许此音频方向' : '';
  }
  profiles = status.qualities;
  if (!capabilityLoaded) {
    const defaults = status.default_quality || status.quality;
    $('quality').value = defaults.id;
    $('mode').value = status.default_mode || (defaults.playback_only ? 'playback' : previousMode);
  }
  capabilityLoaded = true;
  if (!status.modes.includes($('mode').value)) {
    if (recovering) throw Object.assign(new Error('电脑已调整可用音频方向，请重新选择工作模式并点击开始。'), { status: 422 });
    $('mode').value = status.modes[0];
  }
  if (!selectedQuality()) $('quality').value = status.quality.id;
  if (observedInstance && observedInstance !== status.instance_id) resumeID = '';
  observedInstance = status.instance_id || '';
  observedRevision = status.config_revision || 0;
  renderQuality();
  renderMode();
}
function refreshCapabilities() {
  capabilities = loadCapabilities().catch(error => { showError(error.message); if (error.status === 401) clearAuthorization(); });
}
$('save-token').onclick = () => {
  resetPairRequest();token = $('token').value.trim();deviceID='';
  if (!/^[a-f0-9]{48}$/.test(token)) { showError('请输入完整连接凭证，或使用申请配对。'); return; }
  refreshCapabilities();
};
$('request-pair').onclick=async()=>{
 const attempt=++pairGeneration;
 clearTimeout(pairTimer);$('request-pair').disabled=true;$('remember-device').disabled=true;
 const remember=$('remember-device').checked;
 try{
  const ticket=await api('/api/pair/request',{method:'POST',body:JSON.stringify({name:$('device-name').value,remember})});
  if(attempt!==pairGeneration)return;
  $('pair-code').textContent=ticket.code;$('pair-code').hidden=false;
  $('pair-progress').textContent='请在电脑按 p，核对设备名和此确认码，选择允许并回车；申请两分钟内有效。';
  const poll=async()=>{
   if(attempt!==pairGeneration)return;
   try{
    const result=await api('/api/pair/status',{method:'POST',body:JSON.stringify({id:ticket.id,secret:ticket.secret})});
    if(attempt!==pairGeneration)return;
    if(result.state==='pending'){pairTimer=setTimeout(poll,1000);return;}
    if(result.state==='approved'){acceptAuthorization(result);refreshCapabilities();}
    else{showError('电脑拒绝了配对申请，可以重新申请。');}
   }catch(error){if(attempt!==pairGeneration)return;showError(error.message);}
   resetPairRequest();
  };
  pairTimer=setTimeout(poll,1000);
 }catch(error){if(attempt!==pairGeneration)return;showError(error.message);resetPairRequest();}
};
$('forget-device').onclick=async()=>{
 stop();
 try{await api('/api/pair/forget',{method:'POST'});}catch(error){if(error.status!==401){showError(error.message);return;}}
 clearAuthorization();state('尚未连接','已忘记授权；再次使用时请申请配对。');
};
function renderMode() {
  const ws = $('connection').value === 'ws-pcm';
  $('pcm-options').hidden=!ws;
  for (const option of $('mode').options) option.disabled = !availableModes.includes(option.value);
  $('mode-hint').textContent = availableModes.length === 3 ? '可在手机切换三个模式；切换会短暂重新连接。' : '部分模式未启用：请在电脑配置并允许对应音频方向、选择设备后应用。';
  const mode = $('mode').value;
  $('microphone-controls').hidden = !microphoneMode(mode);
  $('playback-controls').hidden = !playbackMode(mode);
  $('duplex-hint').hidden = mode !== 'duplex';
  $('mute').hidden = !microphoneMode(mode);
}
function deleteSession(id) { if (id) api(`/api/session/${id}`, { method: 'DELETE', keepalive: true }).catch(() => {}); }
function cleanup() {
  shortcutControls.close();
  $('black-screen').disabled = true;
  exitBlackScreen();
  const c = current; current = null;
  if (c) {
    c.abort.abort(); clearTimeout(c.connectTimeout); clearTimeout(c.disconnectTimeout); cancelAnimationFrame(c.frame);
    clearInterval(c.feedbackTimer);c.codec?.terminate();c.capture?.disconnect();c.captureSource?.disconnect();
    c.pc?.close(); c.socket?.close(); c.player?.disconnect(); c.gain?.disconnect(); c.stream?.getTracks().forEach(t => t.stop());
    c.context?.close().catch(() => {}); c.wake?.release().catch(() => {});
    deleteSession(c.id);
  }
  remote.pause(); remote.srcObject = null;
  $('resume-playback').hidden = true;
  $('mute').disabled = true;
  $('level').style.width = '0%';
  $('network-info').textContent = '网络统计等待连接';
  document.querySelector('.meter').setAttribute('aria-valuenow', '0');
}
async function wake(c) {
  if (!navigator.wakeLock || document.visibilityState !== 'visible' || current !== c) return false;
  if (c.wake && !c.wake.released) return true;
  if (c.wakeRequest) return c.wakeRequest;
  c.wakeRequest = (async () => {
    try {
      const lock = await navigator.wakeLock.request('screen');
      if (current !== c) { await lock.release(); return false; }
      c.wake = lock;
      lock.addEventListener('release', () => {
        if (current === c) exitBlackScreen('屏幕常亮已失效，请重新进入黑屏模式。');
      });
      return !lock.released;
    } catch (_) { return false; }
  })();
  try { return await c.wakeRequest; } finally { c.wakeRequest = null; }
}
function retry(error) {
  if (!wanted) return;
  generation++; cleanup(); clearTimeout(retryTimer);
  const transientConflict = ['configuration_changed', 'session_replaced'].includes(error.code);
  if (([400, 401, 403, 409, 422].includes(error.status) && !transientConflict) || ['NotAllowedError', 'NotSupportedError', 'NotFoundError', 'NotReadableError', 'OverconstrainedError'].includes(error.name)) {
    stop();
    state(error.status === 401 ? '需要重新配对' : error.code === 'computer_disconnected' ? '电脑端已断开' : error.code === 'session_busy' ? '电脑正在被占用' : '连接已停止', '检查所选设备与电脑端状态后，点击开始重试。'); showError(error.message);
    if (error.status === 401) clearAuthorization();
    return;
  }
  const delay = Math.min(1000 * 2 ** Math.min(retries++, 3), 8000);
  state('正在重新连接', `${Math.round(delay / 1000)} 秒后重试 · 保持页面在前台`, 'busy');
  retryTimer = setTimeout(connect, delay);
}
function pauseMicrophone(c) {
  if (current !== c || c.micPaused) return;
  c.micPaused = true;
  c.stream?.getTracks().forEach(t => { t.enabled = false; t.stop(); });
  $('mute').disabled = true;
  showError('所选麦克风已失效，采集已暂停；不会改用默认麦克风。请检查设备并重新连接。');
}
async function attachMicrophone(c, attempt) {
  c.context = new AudioContext(c.transport==='ws-pcm'?{sampleRate:48000,latencyHint:'interactive'}:undefined);
  await c.context.resume();
  if (attempt !== generation || !wanted) return false;
  const stream = await navigator.mediaDevices.getUserMedia({ audio: {
    deviceId: $('microphone').value ? { exact: $('microphone').value } : undefined,
    echoCancellation: $('echo').checked, noiseSuppression: $('noise').checked,
    autoGainControl: false, channelCount: { ideal: c.quality.channels }, sampleRate: { ideal: 48000 }
  }, video: false });
  if (attempt !== generation || !wanted) { stream.getTracks().forEach(t => t.stop()); return false; }
  c.stream = stream;
  stream.getAudioTracks().forEach(t => { t.enabled = !muted; t.onended = () => pauseMicrophone(c); });
  const devices = await navigator.mediaDevices.enumerateDevices();
  if (attempt !== generation) return false;
  const selected = $('microphone').value;
  $('microphone').replaceChildren(new Option('系统默认麦克风', ''));
  for (const d of devices.filter(d => d.kind === 'audioinput')) $('microphone').add(new Option(d.label || '麦克风', d.deviceId));
  $('microphone').value = selected;
  const source = c.context.createMediaStreamSource(stream);
  const analyser = c.context.createAnalyser(); analyser.fftSize = 256; source.connect(analyser);
  const data = new Uint8Array(analyser.fftSize);
  function draw() {
    if (current !== c) return;
    analyser.getByteTimeDomainData(data);
    let sum = 0;
    for (const value of data) sum += ((value - 128) / 128) ** 2;
    const level = muted || c.micPaused ? 0 : Math.min(100, Math.sqrt(sum / data.length) * 250);
    $('level').style.width = `${level}%`;
    document.querySelector('.meter').setAttribute('aria-valuenow', String(Math.round(level)));
    c.frame = requestAnimationFrame(draw);
  }
  draw(); return true;
}
async function playRemote(c) {
  if (current !== c) return;
  if (c.transport === 'ws-pcm') {
    try {await c.context.resume(); if(current!==c)return; c.playbackPaused=false; pcmVolume(c); $('resume-playback').hidden=true;} catch(error) {showError(error.message);}
    return;
  }
  if (!remote.srcObject) return;
  remote.muted = playbackMuted;
  try {
    await remote.play();
    if (current !== c) return;
    c.playbackPaused = false; $('resume-playback').hidden = true;
  } catch (_) {
    if (current !== c) return;
    c.playbackPaused = true; $('resume-playback').hidden = false;
  }
}
function sessionHeaders(c) {
  return { 'X-RMic-Session': resumeID, 'X-RMic-Config': String(c.revision), 'X-RMic-Instance': c.instance };
}
function connect() {
  clearTimeout(retryTimer);
  const queuedGeneration = generation;
  connectQueue = connectQueue.catch(() => {}).then(async () => {
    if (!wanted || queuedGeneration !== generation) return;
    const pending = connectAttempt();
    const c = current;
    // A suspended browser can leave media promises pending indefinitely. An
    // obsolete attempt must not hold up subsequent clicks on Start or retries.
    let cancelled;
    const aborted = new Promise(resolve => {
      cancelled = resolve;
      if (c.abort.signal.aborted) resolve();
      else c.abort.signal.addEventListener('abort', cancelled, { once: true });
    });
    try { await Promise.race([pending, aborted]); }
    finally { c.abort.signal.removeEventListener('abort', cancelled); }
  });
  return connectQueue;
}
async function connectAttempt() {
  const attempt = ++generation;
  cleanup();
  const c = { transport: $('connection').value, abort: new AbortController(), started: Date.now(), mode: $('mode').value, quality: selectedQuality(), playbackPaused: false };
  current = c;
  $('black-screen').disabled = false;
  state('正在连接', '正在建立所选音频通道…', 'busy');
  $('start').disabled = true; $('stop').hidden = false; $('error').hidden = true; $('directions').hidden = false;
  try {
    await loadCapabilities(true, c.abort.signal);
    if (attempt !== generation || !wanted) return;
    c.mode = $('mode').value; c.quality = selectedQuality();
    c.instance = observedInstance; c.revision = observedRevision;
    c.connectTimeout = setTimeout(() => { if (current === c) retry(new Error('连接超时，请检查网络、证书和播放权限')); }, 22000);
    if (microphoneMode(c.mode) && !await attachMicrophone(c, attempt)) return;
    if (c.transport === 'ws-pcm') {await connectPCM(c, attempt); return;}
    if (attempt !== generation || !wanted) return;
    c.pc = new RTCPeerConnection({ iceServers: [] });
    if (c.stream) {
      c.stream.getTracks().forEach(track => c.pc.addTransceiver(track, { direction: c.mode === 'duplex' ? 'sendrecv' : 'sendonly', streams: [c.stream] }));
    } else c.pc.addTransceiver('audio', { direction: 'recvonly' });
    if (playbackMode(c.mode)) {
      c.pc.ontrack = event => {
        if (current !== c) return;
        let accepted = false;
        try {
          if ('jitterBufferTarget' in event.receiver && selectedBuffers().play_ms) {
            event.receiver.jitterBufferTarget = selectedBuffers().play_ms;
            accepted = event.receiver.jitterBufferTarget === selectedBuffers().play_ms;
          }
        } catch (_) { /* Optional browser playout control. */ }
        if (c.quality.playback_only) {
          c.playoutNote = !selectedBuffers().play_ms ? '手机播放缓冲由 WebRTC 自动调节。' : accepted ? '已向手机申请所选播放缓冲，实际延迟由浏览器决定。' : '此浏览器未接受手机播放缓冲设置，将使用浏览器自动缓冲。';
          renderQuality();
        }
        remote.srcObject = event.streams[0] || new MediaStream([event.track]);
        playRemote(c);
      };
    }
    c.pc.onconnectionstatechange = () => {
      if (current !== c) return;
      if (c.pc.connectionState === 'connected') {
        clearTimeout(c.connectTimeout); clearTimeout(c.disconnectTimeout);
        c.ready = true; manualConnection = false; wake(c);
      }
      if (['failed', 'closed'].includes(c.pc.connectionState)) retry(new Error('音频连接中断'));
      if (c.pc.connectionState === 'disconnected') {
        state('连接暂时中断', '正在尝试恢复连接…', 'busy');
        clearTimeout(c.disconnectTimeout);
        c.disconnectTimeout = setTimeout(() => {
          if (current === c && c.pc.connectionState === 'disconnected') retry(new Error('音频连接未能恢复，正在重新连接'));
        }, 5000);
      }
    };
    await c.pc.setLocalDescription(await c.pc.createOffer());
    await new Promise((resolve, reject) => {
      if (c.pc.iceGatheringState === 'complete') return resolve();
      const done = () => { c.pc.removeEventListener('icegatheringstatechange', changed); c.abort.signal.removeEventListener('abort', aborted); clearTimeout(timeout); };
      const changed = () => { if (c.pc.iceGatheringState === 'complete') { done(); resolve(); } };
      const aborted = () => { done(); reject(new DOMException('Cancelled', 'AbortError')); };
      const timeout = setTimeout(() => { done(); reject(new Error('无法收集本机网络地址')); }, 8000);
      c.pc.addEventListener('icegatheringstatechange', changed); c.abort.signal.addEventListener('abort', aborted, { once: true });
      if (c.abort.signal.aborted) aborted();
    });
    // Consume the response even if stopped, so a late answer can be released.
    const result = await api('/api/session', { method: 'POST', headers: sessionHeaders(c), body: JSON.stringify({ ...c.pc.localDescription.toJSON(), mode: c.mode, quality: c.quality.id, buffers: selectedBuffers() }) });
    if (attempt !== generation || !wanted) { deleteSession(result.id); return; }
    c.id = result.id; resumeID = result.id;
    await c.pc.setRemoteDescription(result.answer);
    for (const sender of c.pc.getSenders()) {
      if (!sender.track) continue;
      const parameters = sender.getParameters();
      if (!parameters.encodings?.length) continue;
      parameters.encodings.forEach(encoding => { encoding.maxBitrate = c.quality.bitrate; });
      try { await sender.setParameters(parameters); } catch (_) { c.qualityNote = '此浏览器未接受发送码率上限，实际采集编码以浏览器能力为准。'; }
    }
    if (current !== c) return;
    if (c.qualityNote) $('quality-hint').textContent += ` ${c.qualityNote}`;
    $('mute').disabled = !c.stream || c.micPaused;
  } catch (error) {
    if (attempt !== generation || !wanted) return;
    const failure = error.name === 'NotAllowedError'
      ? Object.assign(new Error('麦克风权限被拒绝，请在浏览器站点设置中允许访问，并确认 HTTPS 证书已受信任。'), { name: 'NotAllowedError' })
      : error;
    retry(failure);
  }
}
function stop() {
  wanted = false; manualConnection = false; generation++; clearTimeout(retryTimer); cleanup();
  $('start').disabled = false; $('stop').hidden = true; $('directions').hidden = true;
  state('尚未连接', ''); $('timer').textContent = '00:00';
}
$('start').onclick = async () => {
  if (!token) { $('pairing').hidden = false; showError('请先申请配对并在电脑端允许。'); return; }
  if (!window.isSecureContext || ($('connection').value === 'webrtc' && !window.RTCPeerConnection)) { showError('请使用受信任的 HTTPS 地址和新版 Safari、Chrome 或 Edge。'); return; }
  $('start').disabled = true;
  await capabilities;
  if (!token) { $('start').disabled = false; showError('请重新申请配对并在电脑端允许。'); return; }
  if (microphoneMode($('mode').value) && !navigator.mediaDevices?.getUserMedia) { $('start').disabled = false; showError('此浏览器无法采集麦克风。'); return; }
  wanted = true; manualConnection = true; retries = 0; connect();
};
function changeSessionSettings() {
  const restart = wanted;
  stop(); renderMode(); renderQuality();
  if (restart) { wanted = true; manualConnection = true; retries = 0; retryTimer = setTimeout(connect, 150); }
}
$('mode').onchange = changeSessionSettings;
$('quality').onchange = changeSessionSettings;
$('connection').onchange = changeSessionSettings;
$('pcm-compression').onchange=changeSessionSettings;
$('pcm-output').onchange=changeSessionSettings;
$('send-buffer').onchange = changeSessionSettings;
$('play-buffer').onchange = changeSessionSettings;
$('stop').onclick = stop;
$('mute').onclick = () => {
  muted = !muted;
  if (!current?.micPaused) current?.stream?.getAudioTracks().forEach(t => { t.enabled = !muted; });
  $('mute').setAttribute('aria-pressed', String(muted)); $('mute').textContent = muted ? '取消静音' : '静音';
};
$('playback-volume').oninput = () => { remote.volume = Number($('playback-volume').value); pcmVolume(current); };
$('playback-mute').onclick = () => {
  playbackMuted = !playbackMuted;
  remote.muted = playbackMuted || !!current?.playbackPaused;
  pcmVolume(current);
  $('playback-mute').setAttribute('aria-pressed', String(playbackMuted));
  $('playback-mute').textContent = playbackMuted ? '取消播放静音' : '播放静音';
};
$('resume-playback').onclick = () => { if (current) playRemote(current); };
navigator.mediaDevices?.addEventListener('devicechange', async () => {
  const c = current;
  if (!c?.ready) return;
  if (c.transport === 'ws-pcm') {stop();showError('音频设备发生变化，PCM 播放已停止；请确认耳机连接后重新开始。');return;}
  if (playbackMode(c.mode)) {
    remote.pause(); remote.muted = true; c.playbackPaused = true;
    $('resume-playback').hidden = false;
    showError('音频设备发生变化，手机播放已暂停；请确认耳机连接后点击“启用播放”。');
  }
  if (c.stream) {
    try {
      const devices = await navigator.mediaDevices.enumerateDevices();
      const selected = c.stream.getAudioTracks()[0]?.getSettings().deviceId;
      if (current === c && selected && !devices.some(d => d.kind === 'audioinput' && d.deviceId === selected)) pauseMicrophone(c);
    } catch (_) { pauseMicrophone(c); }
  }
});
async function resumeConnection() {
  if (!wanted || document.visibilityState !== 'visible') return;
  const c = current;
  if (!c) { clearTimeout(retryTimer); connect(); return; }
  wake(c);
  if (!c.id) return;
  try {
    const status = await api('/api/status', { signal: c.abort.signal });
    if (current !== c || !wanted) return;
    if (status.state === 'waiting' && status.error === '电脑端已断开连接') { stop(); state('电脑端已断开', '点击开始可以重新连接。'); return; }
    if (status.state === 'waiting' || status.instance_id !== c.instance || status.config_revision !== c.revision || ['failed', 'disconnected', 'closed'].includes(c.pc?.connectionState)) {
      retry(new Error('正在恢复音频连接'));
    } else {
      if (['suspended', 'interrupted'].includes(c.context?.state)) await c.context.resume();
      if (playbackMode(c.mode) && remote.paused) playRemote(c);
    }
  } catch (error) { if (current === c && wanted) retry(error); }
}
document.addEventListener('visibilitychange', resumeConnection);
window.addEventListener('online', resumeConnection);
window.addEventListener('pagehide', stop);
async function networkStats(c) {
  try {
    if (c.transport === 'ws-pcm') {
      const m=c.playbackMetrics;
      $('network-info').textContent=`PCM 48 kHz / ${availableCaptureChannels} 声道 · HTTP RTT ${Math.round(c.rttMS || 0)} ms`+(m ? ` · 手机队列 ${Math.round(m.queued_ms)} ms / 目标 ${Math.round(m.target_ms)} ms · 欠载 ${m.underruns}` : playbackMode(c.mode) ? ' · 正在预缓冲' : ' · 麦克风双声道上行');
      return;
    }
    const reports = await c.pc.getStats();
    if (current !== c) return;
    const values = [...reports.values()];
    const transport = values.find(r => r.type === 'transport' && r.selectedCandidatePairId);
    const pair = transport ? reports.get(transport.selectedCandidatePairId) : values.find(r => r.type === 'candidate-pair' && r.nominated && r.state === 'succeeded');
    const inbound = values.find(r => r.type === 'inbound-rtp' && r.kind === 'audio');
    const pieces = [];
    c.playbackMetrics={queued_ms:Math.min(3000,Number.isFinite(inbound?.jitterBufferDelay) && inbound?.jitterBufferEmittedCount ? 1000*inbound.jitterBufferDelay/inbound.jitterBufferEmittedCount : 0),target_ms:selectedBuffers().play_ms,rtt_ms:Math.min(30000,(pair?.currentRoundTripTime || 0)*1000),underruns:0,received_frames:0};
    if (inbound?.jitterBufferEmittedCount) pieces.push(`接收平均缓冲 ${Math.round(c.playbackMetrics.queued_ms)} ms`);
    if (pair?.currentRoundTripTime != null) pieces.push(`网络往返 ${Math.round(pair.currentRoundTripTime * 1000)} ms`);
    if (inbound?.jitter != null) pieces.push(`接收抖动 ${Math.round(inbound.jitter * 1000)} ms`);
    if (inbound && inbound.packetsReceived + Math.max(0, inbound.packetsLost || 0) > 0) {
      const lost = Math.max(0, inbound.packetsLost || 0);
      pieces.push(`接收丢包 ${(100 * lost / (lost + inbound.packetsReceived)).toFixed(1)}%`);
    }
    if (current !== c) return;
    if (c.id) await api(`/api/session/${c.id}/metrics`,{method:'POST',body:JSON.stringify(c.playbackMetrics),signal:c.abort.signal});
    $('network-info').textContent = pieces.join(' · ') || '网络统计等待采样（不代表音频总延迟）';
  } catch (_) { /* Statistics availability must not interrupt audio. */ }
}
const labels = { disabled: '未启用', idle: '待机', connecting: '连接中', streaming: '正在传音', paused: '已暂停', waiting_audio: '等待音频' };
let lastCapabilityRefresh = Date.now();
setInterval(async () => {
  if (polling) return;
  if (!wanted) {
    if (!token || document.visibilityState !== 'visible' || Date.now() - lastCapabilityRefresh < 3000) return;
    lastCapabilityRefresh = Date.now();
    polling = true;
    capabilities = loadCapabilities().catch(error => {
      if (error.status === 401) { clearAuthorization(); showError(error.message); }
    });
    try { await capabilities; } finally { polling = false; }
    return;
  }
  const c = current;
  if (!c?.id) return;
  polling = true;
  try {
    const requestStarted=performance.now();
    const status = await api('/api/status', { signal: c.abort.signal });
    c.rttMS=Math.min(30000,performance.now()-requestStarted);
    if (current !== c) return;
    shortcutControls.update(status.shortcuts, c.id);
    if ((status.instance_id && status.instance_id !== c.instance) || (status.config_revision && status.config_revision !== c.revision)) {
      retry(new Error('电脑配置已更新，正在重新协商音频通道')); return;
    }
    await networkStats(c);
    if (current !== c) return;
    $('buffer-info').textContent=[microphoneMode(c.mode) ? `电脑麦克风队列 ${Math.round(status.receive.audio.buffered_ms)} ms` : '', playbackMode(c.mode) ? `电脑发送队列 ${Math.round(status.send.audio.buffered_ms)} ms` : ''].filter(Boolean).join(' · ');
    const elapsed = Math.floor((Date.now() - c.started) / 1000);
    $('timer').textContent = `${String(Math.floor(elapsed / 60)).padStart(2, '0')}:${String(elapsed % 60).padStart(2, '0')}`;
    $('receive-state').textContent = !microphoneMode(c.mode) ? '未启用' : c.micPaused ? '设备失效，已暂停' : status.receive.state === 'paused' ? '电脑输出已暂停' : status.receive.muted || muted ? '已静音' : labels[status.receive.state];
    $('send-state').textContent = !playbackMode(c.mode) ? '未启用' : status.send.state === 'paused' ? '电脑采集已暂停' : c.playbackPaused ? '手机播放已暂停，请点击启用播放' : playbackMuted || status.send.muted ? '已静音' : labels[status.send.state];
    const errors = [status.receive.error, status.send.error].filter(Boolean);
    if (errors.length) { showError(errors.join('；')); c.deviceError = true; }
    else if (c.deviceError) { $('error').hidden = true; c.deviceError = false; }
    if (status.state === 'streaming' && (c.transport === 'ws-pcm' ? c.ready : c.pc.connectionState === 'connected')) {
      retries = 0;
      let title = c.mode === 'duplex' ? '双向传音中' : c.mode === 'playback' ? '正在播放' : '正在传音';
      if (errors.length || c.micPaused || c.playbackPaused) title = '部分方向已暂停';
      else if (c.mode === 'mic' && (muted || status.receive.muted)) title = '已静音';
      else if (c.mode === 'playback' && (playbackMuted || status.send.muted)) title = '播放已静音';
      state(title, [microphoneMode(c.mode) ? `麦克风 ${status.receive.device}` : '', playbackMode(c.mode) ? `播放源 ${status.send.device}` : ''].filter(Boolean).join(' · '), 'live');
    } else if (status.state === 'paused') state('音频方向已暂停', errors.join('；'), 'busy');
    else if (status.state === 'waiting' && status.error === '电脑端已断开连接') { stop(); state('电脑端已断开', '点击开始可以重新连接。'); }
    else if (status.state === 'waiting') retry(new Error(status.error || '电脑端已断开连接'));
    else if (status.state === 'waiting_audio') state('等待音频', '连接已建立，但电脑暂未收到音频。', 'busy');
  } catch (error) { if (current === c) retry(error); }
  finally { polling = false; }
}, 1000);
renderMode(); renderQuality(); refreshCapabilities();
