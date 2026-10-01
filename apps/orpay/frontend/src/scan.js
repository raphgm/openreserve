// QR scanner: point the camera at someone's ORPay QR (or a pay link) to
// pay them. Uses the browser's BarcodeDetector where available and jsQR
// otherwise. The camera needs https or localhost.
import jsQR from 'jsqr'
import { isAddress, readPayLink } from './orp.js'

// Turn scanned text into a payment request, or null if it isn't one.
export function parseScan(text) {
  const t = String(text ?? '').trim()
  if (isAddress(t.toLowerCase())) return { to: t.toLowerCase() }
  if (/^@?[a-z0-9_]{3,20}$/i.test(t)) return { to: t.replace(/^@/, '').toLowerCase() }
  try {
    const u = new URL(t)
    return readPayLink(u.search)
  } catch {
    return null
  }
}

export async function openScanner(onResult) {
  if (!navigator.mediaDevices?.getUserMedia) {
    throw new Error('Scanning needs camera access. Open ORPay over https or on this device (localhost).')
  }
  const overlay = document.createElement('div')
  overlay.className = 'scanner'
  overlay.innerHTML = `
    <video playsinline muted></video>
    <div class="scan-frame"><i></i><i></i><i></i><i></i><span class="scan-line"></span></div>
    <p class="scan-hint">Point at an ORPay QR code</p>
    <button class="scan-close" aria-label="Close scanner">✕</button>`
  document.body.append(overlay)
  const video = overlay.querySelector('video')
  let stream, raf, done = false
  const close = () => {
    done = true
    cancelAnimationFrame(raf)
    stream?.getTracks().forEach((t) => t.stop())
    overlay.remove()
  }
  overlay.querySelector('.scan-close').onclick = close
  try {
    stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false })
  } catch {
    close()
    throw new Error('Camera permission was denied. Allow camera access to scan.')
  }
  video.srcObject = stream
  await video.play()

  const detector = 'BarcodeDetector' in window ? new window.BarcodeDetector({ formats: ['qr_code'] }) : null
  const canvas = document.createElement('canvas')
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  const hint = overlay.querySelector('.scan-hint')
  const tick = async () => {
    if (done) return
    let text = null
    try {
      if (detector) {
        text = (await detector.detect(video))[0]?.rawValue ?? null
      } else if (video.videoWidth) {
        canvas.width = video.videoWidth
        canvas.height = video.videoHeight
        ctx.drawImage(video, 0, 0)
        text = jsQR(ctx.getImageData(0, 0, canvas.width, canvas.height).data, canvas.width, canvas.height)?.data ?? null
      }
    } catch {}
    if (text) {
      const req = parseScan(text)
      if (req) {
        navigator.vibrate?.(40)
        close()
        onResult(req)
        return
      }
      hint.textContent = 'That QR is not an ORPay payment code'
    }
    raf = requestAnimationFrame(tick)
  }
  tick()
}
