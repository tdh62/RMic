// Presentation follows the existing session controls. All mode changes still go
// through the native selector and its connection cancellation/recovery handler.
(() => {
  const el = id => document.getElementById(id);
  const mode = el('mode');
  const cards = [...document.querySelectorAll('[data-mode]')];
  const descriptions = {
    mic: '把手机变成电脑的无线麦克风。',
    playback: '用手机收听电脑的声音，无需开启麦克风。',
    duplex: '一边传送手机麦克风，一边收听电脑声音。',
  };
  const text = (node, value) => { if (node.textContent !== value) node.textContent = value; };
  function render() {
    for (const card of cards) {
      const option = [...mode.options].find(option => option.value === card.dataset.mode);
      card.disabled = !option || mode.disabled || option.disabled;
      card.setAttribute('aria-pressed', String(card.dataset.mode === mode.value));
      card.title = card.disabled ? '请在电脑配置中启用此音频方向' : descriptions[card.dataset.mode];
    }
    el('microphone-meter').hidden = mode.value === 'playback';
    const dot = el('dot');
    const kind = dot.classList.contains('live') ? 'live' : dot.classList.contains('busy') ? 'busy' : 'idle';
    el('connection-badge').dataset.state = kind;
    text(el('connection-badge'), kind === 'live' ? '已连接' : kind === 'busy' ? el('status').textContent : el('pairing').hidden ? '待连接' : '待配对');
    text(el('mic-indicator'), el('mute').getAttribute('aria-pressed') === 'true' ? '已静音' : el('mute').disabled ? '等待连接' : '正在采集');
    const quality = el('quality').selectedOptions[0]?.textContent.split(' · ')[0] || '均衡';
    text(el('settings-summary'), quality + (el('connection').value === 'ws-pcm' ? ' · PCM' : ''));
    text(el('volume-value'), `${Math.round(Number(el('playback-volume').value) * 100)}%`);
  }
  for (const card of cards) {
    card.addEventListener('click', () => {
      if (card.disabled || mode.value === card.dataset.mode) return;
      mode.value = card.dataset.mode;
      mode.dispatchEvent(new Event('change', { bubbles: true }));
    });
  }
  document.addEventListener('change', render);
  el('playback-volume').addEventListener('input', render);
  const observer = new MutationObserver(render);
  observer.observe(mode, { subtree: true, attributes: true, childList: true });
  observer.observe(el('quality'), { subtree: true, attributes: true, childList: true });
  for (const id of ['dot', 'mute', 'pairing']) observer.observe(el(id), { attributes: true });
  // Capability defaults may set select.value without firing a change event.
  observer.observe(el('mode-hint'), { childList: true });
  observer.observe(el('quality-hint'), { childList: true });
  el('mode-cards').hidden = false;
  mode.parentElement.classList.add('enhanced');
  render();
})();
