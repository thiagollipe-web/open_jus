// Service worker do OpenJus: guarda o site para funcionar offline (prazos, termos, aprendizado).
// Chamadas à API e a outros domínios (IA local, DataJud) sempre vão à rede.
const CACHE = 'openjus-v1';
const ARQUIVOS = ['./', './index.html', './glossario.json', './manifest.webmanifest',
  './favicon.svg', './icon-192.png', './icon-512.png', './apple-touch-icon.png'];

self.addEventListener('install', e => {
  e.waitUntil(caches.open(CACHE).then(c => c.addAll(ARQUIVOS)).then(() => self.skipWaiting()));
});
self.addEventListener('activate', e => {
  e.waitUntil(caches.keys().then(ks => Promise.all(ks.filter(k => k !== CACHE).map(k => caches.delete(k))))
    .then(() => self.clients.claim()));
});
self.addEventListener('fetch', e => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin || url.pathname.includes('/api/')) return;
  // Rede primeiro (site sempre atualizado); sem internet, usa o cache.
  e.respondWith(fetch(e.request).then(r => {
    const copia = r.clone(); caches.open(CACHE).then(c => c.put(e.request, copia)); return r;
  }).catch(() => caches.match(e.request).then(r => r || caches.match('./index.html'))));
});
