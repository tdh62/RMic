'use strict';
// Keep application files and authenticated API responses on the network so that
// updates take effect together and credentials/audio are never stored in a cache.
self.addEventListener('install', event => event.waitUntil(self.skipWaiting()));
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', event => {
  if (event.request.mode !== 'navigate') return;
  // Do not turn TLS failures into an offline page during certificate recovery.
  // Leaving navigation unhandled allows the browser to offer its trust prompt.
  if (new URL(event.request.url).searchParams.has('rmic-trust')) return;
  event.respondWith(fetch(event.request).catch(() => new Response(
    '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RMic</title><body style="background:#111a20;color:#edf6f7;font:16px system-ui;padding:24px"><h1>无法连接电脑</h1><p>检查网络和电脑端是否运行，然后重试。</p><button onclick="location.reload()">重新连接</button><p>切换 IP 或证书更新后，可能需要在浏览器中重新确认证书。请核对地址，在浏览器提示中选择“高级”并继续访问。</p><a href="/?rmic-trust=1" onclick="this.href=\'/?rmic-trust=\'+Date.now()" style="color:#8de0df">重新信任证书</a></body></html>',
    { headers: { 'Content-Type': 'text/html; charset=utf-8' } }
  )));
});
