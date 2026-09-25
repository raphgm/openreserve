// Public landing page for visitors without a wallet on this device.
// Structure: nav, hero with wallet preview, trust strip, features, escrow
// flow, transparency, developers, call to action.
import './landing.css'

const svg = (d) => `<svg viewBox="0 0 24 24" aria-hidden="true">${d}</svg>`
const I = {
  up: '<path d="M12 19V5M6 11l6-6 6 6"/>',
  down: '<path d="M12 5v14M6 13l6 6 6-6"/>',
  escrow: '<path d="M12 3 4.5 6v5.5c0 4.6 3.2 8.2 7.5 9.5 4.3-1.3 7.5-4.9 7.5-9.5V6z"/><path d="m9 12 2 2 4-4"/>',
  ajo: '<circle cx="12" cy="12" r="8"/><path d="M12 8v4l3 2"/>',
  eye: '<path d="M2.5 12S6 5 12 5s9.5 7 9.5 7-3.5 7-9.5 7-9.5-7-9.5-7z"/><circle cx="12" cy="12" r="3"/>',
  check: '<path d="m5 12.5 4.5 4.5L19 7.5"/>',
}

export function renderLanding(app, { onStart, onSignIn, signInLabel }) {
  document.title = 'ORPay: payments on OpenReserve'
  app.innerHTML = `
  <div class="lp">
    <nav class="lp-nav">
      <a class="logo" href="#top" aria-label="ORPay"><span class="logo-pay">Pay</span></a>
      <div class="lp-links">
        <a href="#features">Features</a><a href="#escrow">How it works</a><a href="#developers">Developers</a><a href="/explorer.html">Explorer</a>
      </div>
      <div class="lp-nav-cta">
        <button class="lp-btn ghost" data-signin>${signInLabel}</button>
        <button class="lp-btn ink" data-start>Get started</button>
      </div>
    </nav>

    <section class="lp-hero" id="top">
      <div class="lp-glow"></div>
      <div class="lp-wrap lp-hero-grid">
        <div>
          <span class="lp-pill"><span class="dot on"></span><span id="lp-live">Live on the OpenReserve network</span></span>
          <h1>Money should<br><span class="lp-grad">just work.</span></h1>
          <p class="lp-lead">Send money, save in ajo and pay through escrow. Every step visible.</p>
          <div class="lp-cta">
            <button class="lp-btn ink lg" data-start>Create a wallet</button>
            <a class="lp-btn ghost lg" href="#escrow">Explore payments</a>
          </div>
          <ul class="lp-ticks">
            <li>${svg(I.check)}Transparent settlement</li><li>${svg(I.check)}Escrow built in</li><li>${svg(I.check)}Flat ₦20 fee</li>
          </ul>
        </div>

        <div class="lp-phone-wrap" aria-hidden="true">
          <div class="lp-phone">
            <i class="side a"></i><i class="side b"></i><i class="side c"></i><i class="side d"></i>
            <div class="lp-screen">
              <div class="lp-status"><b>9:41</b><span class="island"></span><span class="lp-sig"><i></i><i></i><i></i><i></i><span class="batt"><span></span></span></span></div>
              <div class="lp-apphead"><span class="logo"><span class="logo-pay">Pay</span></span><span class="me">RG</span></div>
              <div class="lp-bal">
                <small>Available balance</small>
                <strong>₦248,500</strong>
                <span>+₦45,000 today</span>
              </div>
              <div class="lp-actions">
                <span><i class="c1">${svg(I.up)}</i>Send</span>
                <span><i class="c2">${svg(I.down)}</i>Receive</span>
                <span><i class="c3">${svg(I.escrow)}</i>Escrow</span>
                <span><i class="c4">${svg(I.ajo)}</i>Ajo</span>
              </div>
              <div class="lp-recent-head"><b>Recent activity</b><span>See all</span></div>
              <div class="lp-tx"><i class="in">${svg(I.down)}</i><span><b>Ajo payout</b><small>Today · 9:42 AM</small></span><em class="pos">+₦45,000</em></div>
              <div class="lp-tx"><i class="out">${svg(I.escrow)}</i><span><b>Escrow · iPhone 14</b><small>Yesterday · 4:20 PM</small></span><em>−₦42,000</em></div>
              <div class="lp-tabs"><span class="on">Home</span><span>Pools</span><span>Escrow</span><span>Profile</span></div>
              <span class="home-ind"></span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="lp-strip"><div class="lp-wrap"><span>OpenReserve</span><span>Gabis Payments</span><span>Paynautik</span><span>SSLabs</span><span>ORPay</span></div></section>

    <section class="lp-wrap lp-sec" id="features">
      <p class="lp-eyebrow">Payments, done right</p>
      <h2>Everything you need<br>to move money.</h2>
      <div class="lp-cards">
        <article class="reveal"><i class="c1">${svg(I.up)}</i><h3>Send instantly</h3><p>Pay anyone by @username or QR.</p></article>
        <article class="reveal"><i class="c3">${svg(I.escrow)}</i><h3>Built-in escrow</h3><p>Funds wait until both sides are happy.</p></article>
        <article class="reveal"><i class="c4">${svg(I.ajo)}</i><h3>Ajo pools</h3><p>See who paid and who's next.</p></article>
        <article class="reveal"><i class="c2">${svg(I.eye)}</i><h3>Open ledger</h3><p>Every payment can be verified.</p></article>
      </div>
    </section>

    <section class="lp-band" id="escrow">
      <div class="lp-wrap lp-sec center">
        <p class="lp-eyebrow">Escrow</p>
        <h2>Payments with <span class="lp-grad">less drama.</span></h2>
        <p class="lp-sub">Money stays protected until the deal is done. No returns, so buyers accept the terms first.</p>
        <ol class="lp-steps">
          <li class="reveal"><span>1</span><h3>Lock</h3><p>Buyer puts the amount into escrow.</p></li>
          <li class="reveal"><span>2</span><h3>Inspect</h3><p>Buyer receives and checks the goods.</p></li>
          <li class="reveal"><span>3</span><h3>Release</h3><p>Seller is paid when it's complete.</p></li>
        </ol>
      </div>
    </section>

    <section class="lp-wrap lp-sec lp-split">
      <div>
        <p class="lp-eyebrow">Transparency</p>
        <h2>Know where<br>your money is.</h2>
        <p class="lp-sub left">Every payment has a clear state. No guessing, no hidden steps.</p>
      </div>
      <div class="lp-track reveal">
        <div class="lp-track-head"><span><small>Transaction</small><b>iPhone 14 · Escrow</b></span><em>Delivered</em></div>
        <div class="lp-track-steps">
          <span class="done"><i>${svg(I.check)}</i>Locked</span><b></b>
          <span class="done green"><i>${svg(I.check)}</i>Delivered</span><b class="dim"></b>
          <span><i>3</i>Released</span>
        </div>
        <div class="lp-track-amt"><span>Escrow amount</span><strong>₦420,000</strong></div>
      </div>
    </section>

    <section class="lp-dark" id="developers">
      <div class="lp-wrap lp-sec lp-split">
        <div>
          <p class="lp-eyebrow light">For developers</p>
          <h2>Put payments<br>inside your app.</h2>
          <p class="lp-sub left light">Checkout, escrow and signed webhooks. Your brand beside ours.</p>
          <button class="lp-btn white" data-start>Request API access</button>
        </div>
        <pre class="lp-code"><code><span class="k">import</span> { ORPay } <span class="k">from</span> <span class="s">'@openreserve/orpay'</span>

<span class="k">const</span> orpay = <span class="k">new</span> ORPay({ apiKey: process.env.ORPAY_KEY })

<span class="k">const</span> escrow = <span class="k">await</span> orpay.createEscrow({
  seller: <span class="s">'@gadgethub'</span>, currency: <span class="s">'NGN'</span>,
  description: <span class="s">'iPhone 14'</span>,
  milestones: [{ label: <span class="s">'Item'</span>, amount: <span class="s">'420000'</span> }],
})
<span class="c">// the buyer funds escrow.url</span></code></pre>
      </div>
    </section>

    <section class="lp-wrap">
      <div class="lp-final">
        <p class="lp-eyebrow light">OpenReserve</p>
        <h2>Your money.<br>Your visibility.</h2>
        <p>Start with a free wallet. Takes a minute.</p>
        <button class="lp-btn white lg" data-start>Create your wallet</button>
      </div>
    </section>

    <footer class="lp-wrap lp-foot">
      <span class="logo" aria-label="ORPay"><span class="logo-pay">Pay</span></span>
      <span class="lp-proto"><img src="/favicon.svg" alt="">Built on the <b>OpenReserve Protocol</b></span>
      <a href="/explorer.html">Explorer →</a>
    </footer>
    <p class="lp-disclose">Test network · No real funds yet</p>
  </div>`

  app.querySelectorAll('[data-start]').forEach((b) => (b.onclick = onStart))
  app.querySelectorAll('[data-signin]').forEach((b) => (b.onclick = onSignIn))

  const io = 'IntersectionObserver' in window && new IntersectionObserver((es) => es.forEach((e) => e.isIntersecting && (e.target.classList.add('in'), io.unobserve(e.target))), { threshold: 0.15 })
  app.querySelectorAll('.reveal').forEach((el, i) => (io ? ((el.style.transitionDelay = `${(i % 4) * 70}ms`), io.observe(el)) : el.classList.add('in')))

  const wrap = app.querySelector('.lp-phone-wrap'), phone = app.querySelector('.lp-phone')
  if (wrap && matchMedia('(hover: hover)').matches) {
    wrap.onpointermove = (e) => {
      const r = wrap.getBoundingClientRect()
      const x = (e.clientX - r.left) / r.width - 0.5, y = (e.clientY - r.top) / r.height - 0.5
      phone.style.transform = `perspective(1000px) rotateY(${x * 10}deg) rotateX(${-y * 8}deg)`
    }
    wrap.onpointerleave = () => (phone.style.transform = '')
  }
  fetch('/v1/status').then((r) => r.json()).then((st) => {
    const el = document.getElementById('lp-live')
    if (el && st.height > 0) el.textContent = `Live on OpenReserve · block ${st.height.toLocaleString()}`
  }).catch(() => {})
}
