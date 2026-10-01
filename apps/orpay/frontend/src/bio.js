// Face ID / Touch ID / fingerprint unlock using a passkey (WebAuthn) with
// the PRF extension. The passkey produces a secret only after the person
// passes the biometric check; that secret encrypts the wallet key on this
// device. Nothing biometric ever leaves the phone, and ORPay never sees it.
import { fromHex, toHex } from './orp.js'

const KEY = 'orpay.bio.v1'
const enc = new TextEncoder()
const PRF_SALT = enc.encode('orpay-wallet-unlock-v1')

export const bioEnabled = () => {
  try {
    return !!JSON.parse(localStorage.getItem(KEY))
  } catch {
    return false
  }
}

export async function bioSupported() {
  return !!(window.PublicKeyCredential && (await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable?.()))
}

async function aesKey(prf) {
  const hk = await crypto.subtle.importKey('raw', prf, 'HKDF', false, ['deriveKey'])
  return crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt: new Uint8Array(32), info: enc.encode('orpay bio key') },
    hk, { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt'],
  )
}

export async function enableBio(seed, address, label) {
  if (!(await bioSupported())) throw new Error('This device has no Face ID, Touch ID or fingerprint available to the browser.')
  const cred = await navigator.credentials.create({
    publicKey: {
      challenge: crypto.getRandomValues(new Uint8Array(32)),
      rp: { name: 'ORPay' },
      user: { id: fromHex(address).slice(0, 32), name: label, displayName: label },
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }, { type: 'public-key', alg: -257 }],
      authenticatorSelection: { authenticatorAttachment: 'platform', userVerification: 'required', residentKey: 'preferred' },
      extensions: { prf: { eval: { first: PRF_SALT } } },
    },
  })
  let prf = cred.getClientExtensionResults().prf
  if (!prf?.enabled) throw new Error('This browser cannot use Face ID / fingerprint to protect keys yet. Try the latest Chrome or Safari.')
  // Some authenticators only return the secret on sign-in, not on creation.
  const secret = prf.results?.first ?? (await getSecret(new Uint8Array(cred.rawId)))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await aesKey(secret), seed))
  localStorage.setItem(KEY, JSON.stringify({ id: toHex(new Uint8Array(cred.rawId)), iv: toHex(iv), ct: toHex(ct), address }))
}

async function getSecret(rawId) {
  const a = await navigator.credentials.get({
    publicKey: {
      challenge: crypto.getRandomValues(new Uint8Array(32)),
      allowCredentials: [{ type: 'public-key', id: rawId }],
      userVerification: 'required',
      extensions: { prf: { eval: { first: PRF_SALT } } },
    },
  })
  const out = a.getClientExtensionResults().prf?.results?.first
  if (!out) throw new Error('Face ID / fingerprint unlock is not available in this browser.')
  return out
}

export async function unlockBio() {
  const v = JSON.parse(localStorage.getItem(KEY))
  const secret = await getSecret(fromHex(v.id))
  try {
    return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: fromHex(v.iv) }, await aesKey(secret), fromHex(v.ct)))
  } catch {
    throw new Error('Could not unlock with Face ID / fingerprint. Use your password.')
  }
}

export function disableBio() {
  localStorage.removeItem(KEY)
}
