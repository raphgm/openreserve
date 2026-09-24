// ORPay service worker: makes the app installable and lets its shell load
// offline. It never caches API traffic (/v1, /api, /pay): balances and
// payments must always come fresh from the network.
const CACHE = 'orpay-shell-v1'
const SHELL = ['/', '/index.html', '/manifest.webmanifest', '/icon-192.png', '/icon-512.png', '/favicon.svg']

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()))
})

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url)
  if (e.request.method !== 'GET' || url.origin !== location.origin) return
  if (/^\/(v1|api|pay)\//.test(url.pathname)) return // always network
  if (url.pathname.startsWith('/assets/')) {
    // Hashed build files never change: cache first.
    e.respondWith(
      caches.match(e.request).then((hit) => hit ?? fetch(e.request).then((res) => {
        const copy = res.clone()
        caches.open(CACHE).then((c) => c.put(e.request, copy))
        return res
      })),
    )
    return
  }
  // Pages: network first so updates arrive, cache as offline fallback.
  e.respondWith(
    fetch(e.request)
      .then((res) => {
        if (res.ok && e.request.mode === 'navigate') {
          const copy = res.clone()
          caches.open(CACHE).then((c) => c.put('/index.html', copy))
        }
        return res
      })
      .catch(() => caches.match(e.request).then((hit) => hit ?? caches.match('/index.html'))),
  )
})
