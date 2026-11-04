'use strict';
let installPrompt = null, blackFullscreen = false, enteringBlack = false;
const blackOverlay = $('black-overlay');
const displayHint = text => {
  $('display-hint').textContent = text;
  $('display-hint').hidden = !text;
};
function exitBlackScreen(message = '') {
  if (!blackOverlay.open) return;
  blackOverlay.close();
  document.body.classList.remove('black-screen');
  document.querySelector('meta[name="theme-color"]').content = '#111a20';
  if (blackFullscreen && document.fullscreenElement === blackOverlay) document.exitFullscreen().catch(() => {});
  blackFullscreen = false;
  if (message) displayHint(message);
}
$('black-screen').onclick = async () => {
  if (!wanted || !current || enteringBlack) return;
  enteringBlack = true;
  const c = current;
  displayHint('黑屏后点按任意位置退出。请勿按电源键锁屏。');
  blackOverlay.showModal();
  document.body.classList.add('black-screen');
  document.querySelector('meta[name="theme-color"]').content = '#000000';
  // Request fullscreen during the user gesture; standalone PWA also works
  // without the optional Fullscreen API.
  const fullscreen = blackOverlay.requestFullscreen ? blackOverlay.requestFullscreen().then(() => {
    if (blackOverlay.open) blackFullscreen = true;
    else if (document.fullscreenElement === blackOverlay) return document.exitFullscreen();
  }).catch(() => {}) : Promise.resolve();
  const awake = await wake(c);
  if (current === c && blackOverlay.open && !awake) exitBlackScreen('无法保持屏幕常亮，请检查浏览器权限或省电设置后重试。');
  await fullscreen;
  enteringBlack = false;
};
$('exit-black-screen').onclick = () => exitBlackScreen();
blackOverlay.addEventListener('cancel', event => { event.preventDefault(); exitBlackScreen(); });
document.addEventListener('fullscreenchange', () => {
  if (blackFullscreen && document.fullscreenElement !== blackOverlay) exitBlackScreen();
});
window.addEventListener('beforeinstallprompt', event => {
  event.preventDefault();
  installPrompt = event;
  $('install-app').hidden = false;
});
$('install-app').onclick = async () => {
  if (!installPrompt) return;
  const prompt = installPrompt;
  installPrompt = null;
  $('install-app').hidden = true;
  try { await prompt.prompt(); await prompt.userChoice; }
  catch (_) { displayHint('请通过浏览器菜单安装到主屏幕。'); }
};
window.addEventListener('appinstalled', () => { installPrompt = null; $('install-app').hidden = true; });
if ('serviceWorker' in navigator && window.isSecureContext) {
  navigator.serviceWorker.register('/sw.js', { updateViaCache: 'none' }).catch(() => {
    displayHint('安装支持未能启用，请确认 HTTPS 证书已受信任。');
  });
}
