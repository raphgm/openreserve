// Stores the wallet seed in localStorage, encrypted with a password
// (PBKDF2-SHA256 -> AES-GCM). The decrypted seed only ever lives in memory.
import { fromHex, toHex } from './orp.js'

const KEY = 'orpay.vault.v1'
const ITERATIONS = 310_000

async function deriveKey(password, salt) {
  const base = await crypto.subtle.importKey('raw', new TextEncoder().encode(password), 'PBKDF2', false, ['deriveKey'])
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations: ITERATIONS, hash: 'SHA-256' },
    base,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
}

export function hasVault() {
  return localStorage.getItem(KEY) !== null
}

export function vaultAddress() {
  try {
    return JSON.parse(localStorage.getItem(KEY))?.address ?? null
  } catch {
    return null
  }
}

export async function saveVault(seed, address, password) {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const key = await deriveKey(password, salt)
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, seed))
  localStorage.setItem(KEY, JSON.stringify({ address, salt: toHex(salt), iv: toHex(iv), ct: toHex(ct) }))
}

export async function unlockVault(password) {
  const v = JSON.parse(localStorage.getItem(KEY))
  const key = await deriveKey(password, fromHex(v.salt))
  try {
    const seed = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: fromHex(v.iv) }, key, fromHex(v.ct))
    return new Uint8Array(seed)
  } catch {
    throw new Error('Wrong password')
  }
}

export function clearVault() {
  localStorage.removeItem(KEY)
}
