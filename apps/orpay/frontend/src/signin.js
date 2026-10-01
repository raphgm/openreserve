// Six-word sign-in: open your wallet on any device with @username + six
// words. The words never leave the device: they derive an encryption key
// (for the wallet key, stored encrypted on ORPay) and a login token. The
// 24 recovery words stay the offline backup.
import { wordlist } from '@scure/bip39/wordlists/english'
import { fromHex, toHex } from './orp.js'

const ROUNDS = 600_000

export function newSignInWords() {
  const idx = crypto.getRandomValues(new Uint16Array(6))
  return Array.from(idx, (n) => wordlist[n % 2048]) // 65536 is a multiple of 2048: no bias
}

export function parseSignInWords(text) {
  const words = text.toLowerCase().trim().split(/\s+/).filter(Boolean)
  if (words.length !== 6) throw new Error('Enter exactly six words')
  const bad = words.find((w) => !wordlist.includes(w))
  if (bad) throw new Error(`"${bad}" is not one of the sign-in words. Check the spelling.`)
  return words
}

async function derive(words, salt) {
  const base = await crypto.subtle.importKey('raw', new TextEncoder().encode(words.join(' ')), 'PBKDF2', false, ['deriveBits'])
  const bits = new Uint8Array(await crypto.subtle.deriveBits({ name: 'PBKDF2', salt, iterations: ROUNDS, hash: 'SHA-256' }, base, 512))
  const enc = await crypto.subtle.importKey('raw', bits.slice(0, 32), 'AES-GCM', false, ['encrypt', 'decrypt'])
  return { enc, auth: toHex(bits.slice(32)) }
}

// Encrypt the wallet key for upload.
export async function sealSignIn(seed, words) {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const { enc, auth } = await derive(words, salt)
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, enc, seed))
  return { salt: toHex(salt), iv: toHex(iv), ct: toHex(ct), auth }
}

export async function signInToken(words, saltHex) {
  return (await derive(words, fromHex(saltHex))).auth
}

export async function unsealSignIn(words, { salt, iv, ct }) {
  const { enc } = await derive(words, fromHex(salt))
  try {
    return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: fromHex(iv) }, enc, fromHex(ct)))
  } catch {
    throw new Error('Those words do not match this wallet')
  }
}
