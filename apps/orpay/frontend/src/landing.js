// Public landing page for visitors without a wallet on this device.
// Structure: nav, hero with wallet preview, trust strip, features, escrow
// flow, transparency, developers, call to action.
import './landing.css'
import { API_BASE } from './orp.js'

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
      <a class="logo" href="#top" aria-label="ORPay"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></a>
      <div class="lp-links">
        <a href="#product">Product</a><a href="#escrow">Escrow</a><a href="#developers">Developers</a><a href="/explorer.html">Explorer</a>
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
          <h1>Payments without<br><span class="lp-grad">the uncertainty.</span></h1>
          <ul class="lp-triad">
            <li><span>01</span>Send money.</li>
            <li><span>02</span>Protect transactions.</li>
            <li><span>03</span>Release when it's done.</li>
          </ul>
          <div class="lp-cta">
            <button class="lp-btn ink lg" data-start>Get started</button>
            <a class="lp-btn ghost lg" href="#escrow">Explore escrow</a>
          </div>
        </div>

        <div class="lp-phone-wrap" aria-hidden="true">
          <div class="lp-phone">
            <i class="side a"></i><i class="side b"></i><i class="side c"></i><i class="side d"></i>
            <div class="lp-screen">
              <div class="lp-status"><b>9:41</b><span class="island"></span><span class="lp-sig"><i></i><i></i><i></i><i></i><span class="batt"><span></span></span></span></div>
              <div class="lp-apphead"><span class="logo"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></span><span class="me">RG</span></div>
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

    <section class="lp-strip">
      <div class="lp-wrap">
        <span>${svg('<path d="M4 7h16M4 12h16M4 17h10"/>')}OpenReserve</span>
        <span>${svg(I.escrow)}Secure</span>
        <span>${svg(I.eye)}Transparent</span>
        <span>${svg('<path d="m8 8-4 4 4 4M16 8l4 4-4 4"/>')}API</span>
      </div>
    </section>

    <section class="lp-wrap lp-sec center" id="product">
      <h2>One payment.<br><span class="lp-grad">Every step visible.</span></h2>
      <div class="lp-trio">
        <article class="reveal t-send">
          <div class="t-visual">
            <div class="t-chip"><span class="t-av" style="--h:262">AO</span><span><b>@adaeze</b><small>Friend</small></span></div>
            <div class="t-amount">₦25,000</div>
            <div class="t-pay">Send ${svg(I.up)}</div>
          </div>
          <div class="t-head"><i class="c1">${svg(I.up)}</i><h3>Send</h3></div><p>Pay anyone by @username or QR in seconds.</p>
        </article>
        <article class="reveal t-escrow feature">
          <div class="t-visual">
            <div class="t-lock">${svg('<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>')}</div>
            <div class="t-held"><small>Held in escrow</small><b>₦420,000</b></div>
            <div class="t-steps"><span class="on"></span><span class="on"></span><span class="now"></span><span></span></div>
          </div>
          <div class="t-head"><i class="c3">${svg(I.escrow)}</i><h3>Escrow</h3></div><p>Money waits until the goods arrive.</p>
        </article>
        <article class="reveal t-recv">
          <div class="t-visual">
            <div class="t-note"><span class="t-ok">${svg(I.down)}</span><span><b>Payment received</b><small>from @tunde · just now</small></span><em>+₦45,000</em></div>
            <div class="t-note ghost"><span class="t-ok">${svg(I.down)}</span><span><b>Ajo payout</b><small>Market women ajo</small></span><em>+₦60,000</em></div>
          </div>
          <div class="t-head"><i class="c2">${svg(I.down)}</i><h3>Receive</h3></div><p>Get paid and see it land instantly.</p>
        </article>
      </div>
    </section>

    <section class="lp-life" id="escrow">
      <div class="lp-wrap lp-sec center">
        <p class="lp-eyebrow light">Escrow lifecycle</p>
        <h2>From pending to released.</h2>
        <div class="lp-flow reveal">
          <div class="lp-flow-card">
            <div class="lp-flow-top"><span><small>Escrow payment</small><b>iPhone 14 Pro</b></span><strong>₦420,000</strong></div>
            <ol class="lp-flow-steps">
              <li class="done"><i>${svg(I.check)}</i><b>Pending</b><small>Terms accepted</small></li>
              <li class="done"><i>${svg(I.check)}</i><b>Funded</b><small>Money locked</small></li>
              <li class="now"><i></i><b>Shipped</b><small>On its way</small></li>
              <li><i></i><b>Released</b><small>Seller paid</small></li>
            </ol>
          </div>
        </div>
        <p class="lp-sub light">Nobody can move locked money. Not the seller, not the buyer, not us. Disputes go to a neutral arbiter.</p>
      </div>
    </section>

    <section class="lp-dark" id="developers">
      <div class="lp-wrap lp-sec lp-split">
        <div>
          <p class="lp-eyebrow light">Built for businesses</p>
          <h2>Payments inside<br>your app.</h2>
          <ul class="lp-biz">
            <li>${svg(I.check)}Checkout and escrow APIs</li>
            <li>${svg(I.check)}Signed webhooks</li>
            <li>${svg(I.check)}Your logo beside ours</li>
          </ul>
          <button class="lp-btn white" data-start>Request API access</button>
        </div>
        <pre class="lp-code"><code><span class="k">import</span> { ORPay } <span class="k">from</span> <span class="s">'@openreservepolicy/orpay'</span>

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
      <div class="lp-final v2">
        <span class="lp-pill dark"><span class="dot on"></span>Live on the OpenReserve network</span>
        <h2>Money you can <span class="lp-grad light">see and trust.</span></h2>
        <p>Start saving with your circle or selling with escrow in under a minute.</p>
        <div class="f-cta">
          <button class="lp-btn white lg" data-start>Create a free wallet</button>
          <a class="lp-btn glass lg" href="#developers">For businesses →</a>
        </div>
        <ul class="f-stats">
          <li><b>₦20</b><span>flat fee</span></li>
          <li><b>0</b><span>keys we hold</span></li>
          <li><b>100%</b><span>on-chain proof</span></li>
        </ul>
      </div>
    </section>

    <footer class="lp-wrap lp-foot">
      <span class="logo" aria-label="ORPay"><span class="logo-or">OR</span><span class="logo-pay">Pay</span></span>
      <span class="lp-proto"><img src="/favicon.png" alt="">Built on the <b>OpenReserve Protocol</b></span>
      <a href="/explorer.html">Explorer →</a>
    </footer>
  </div>`

  app.querySelectorAll('[data-start]').forEach((b) => (b.onclick = onStart))
  app.querySelectorAll('[data-signin]').forEach((b) => (b.onclick = onSignIn))

  const io = 'IntersectionObserver' in window && new IntersectionObserver((es) => es.forEach((e) => e.isIntersecting && (e.target.classList.add('in'), io.unobserve(e.target))), { threshold: 0.15 })
  app.querySelectorAll('.reveal').forEach((el, i) => (io ? ((el.style.transitionDelay = `${(i % 4) * 70}ms`), io.observe(el)) : el.classList.add('in')))

  fetch(API_BASE + '/v1/status').then((r) => r.json()).then((st) => {
    const el = document.getElementById('lp-live')
    if (el && st.height > 0) el.textContent = `Live on OpenReserve · block ${st.height.toLocaleString()}`
  }).catch(() => {})
}
