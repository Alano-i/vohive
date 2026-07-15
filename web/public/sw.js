const CACHE_NAME = 'vohive-shell-v1'
const STATIC_DESTINATIONS = new Set(['style', 'script', 'font', 'image', 'manifest'])

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys.filter(key => key !== CACHE_NAME).map(key => caches.delete(key))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', event => {
  const request = event.request
  if (request.method !== 'GET') return

  const url = new URL(request.url)
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return
  if (!STATIC_DESTINATIONS.has(request.destination)) return

  event.respondWith(
    caches.open(CACHE_NAME).then(async cache => {
      const cached = await cache.match(request)
      const fresh = fetch(request)
        .then(response => {
          if (response.ok) cache.put(request, response.clone())
          return response
        })
        .catch(error => {
          if (cached) return cached
          throw error
        })
      return cached || fresh
    })
  )
})
