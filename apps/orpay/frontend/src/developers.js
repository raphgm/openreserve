// Developer portal: companies (Gabis, Paynautik, or anyone else) request to
// use OpenReserve payments, admins review requests, and approved apps manage
// API keys, webhooks and their settlement address.
import { api, isAddress, partnerMark } from './orp.js'

let ctx

export function initDevelopers(c) {
  ctx = c
}

const statusChip = (s) =>
  ({
    pending: '<span class="chip-s warn">Pending review</span>',
    approved: '<span class="chip-s ok">Approved</span>',
    rejected: '<span class="chip-s bad">Rejected</span>',
    suspended: '<span class="chip-s bad">Suspended</span>',
  })[s] ?? ''

export async function renderDevelopers() {
  const root = ctx.root()
  root.innerHTML = '<section class="card"><p class="muted">Loading…</p></section>'
  let data
  try {
    data = await api.apps(ctx.state.seed)
  } catch (err) {
    root.innerHTML = `<section class="card"><h2>Developers</h2><p class="error">${ctx.esc(err.message)}</p></section>`
    return
  }
  const mine = data.apps.filter((a) => a.owner === ctx.state.address)
  const review = data.is_admin ? data.apps : []

  root.innerHTML = `
    <section class="card dev-hero">
      <h2>Accept ORP in your app</h2>
      <p class="muted">Let customers pay with ORPay in your website or app. Create a checkout from your server, send customers to it, and get a signed webhook when they pay. Money settles straight to your wallet.</p>
      <button class="primary small" id="request">Request access</button>
    </section>
    ${mine.map(appCard).join('')}
    ${data.is_admin ? `
    <section class="card">
      <h2>Review requests <span class="chip-s info">Admin</span></h2>
      <ul class="review-list">${review.length ? review.map(reviewRow).join('') : '<li class="empty">No apps yet.</li>'}</ul>
    </section>` : ''}`

  ctx.$('#request').onclick = renderRequest
  bindAppCards(mine)
  root.querySelectorAll('[data-review]').forEach((b) => {
    b.onclick = async () => {
      const [id, decision] = b.dataset.review.split(':')
      const note = decision === 'approve' ? '' : prompt(`Reason to ${decision} (shown to the company):`) ?? ''
      b.disabled = true
      try {
        await api.reviewApp(ctx.state.seed, id, decision, note)
        ctx.toast(`App ${decision === 'approve' ? 'approved' : decision + 'ed'}`)
        renderDevelopers()
      } catch (err) {
        ctx.toast(err.message, 'err')
        b.disabled = false
      }
    }
  })
}

function reviewRow(a) {
  const actions = {
    pending: ['approve', 'reject'],
    approved: ['suspend'],
    suspended: ['approve'],
    rejected: ['approve'],
  }[a.status]
  return `
    <li>
      <span class="who">
        <strong>${ctx.esc(a.name)} ${statusChip(a.status)}</strong>
        <small>${ctx.esc(a.website)} · ${ctx.esc(a.contact_email)} · requested ${new Date(a.created_at).toLocaleDateString()}</small>
        ${a.description ? `<small class="desc">${ctx.esc(a.description)}</small>` : ''}
      </span>
      <span class="row-actions">${actions
        .map((d) => `<button class="${d === 'approve' ? 'primary' : 'danger'} small" data-review="${a.id}:${d}">${d[0].toUpperCase() + d.slice(1)}</button>`)
        .join('')}</span>
    </li>`
}

function appCard(a) {
  const approved = a.status === 'approved'
  return `
    <section class="card app-card" data-app="${a.id}">
      <div class="section-head"><h2>${ctx.esc(a.name)}</h2>${statusChip(a.status)}</div>
      <p class="muted small-text">${ctx.esc(a.website)} · App ID <span class="mono">${a.id}</span></p>
      ${a.review_note ? `<p class="banner">${ctx.esc(a.review_note)}</p>` : ''}
      ${a.status === 'pending' ? '<p class="muted">Your request is being reviewed. You will be able to create API keys once it is approved.</p>' : ''}
      ${approved ? `
      <div class="stack">
        <div>
          <p class="label">API key</p>
          <p class="small-text">${a.key_prefix ? `<span class="mono">${ctx.esc(a.key_prefix)}…</span> (only shown once when created)` : 'No key yet.'}</p>
          <div class="key-out" hidden></div>
          <button class="ghost small" data-act="key">${a.key_prefix ? 'Roll key' : 'Create API key'}</button>
        </div>
        <label>Webhook URL<input data-field="webhook_url" value="${ctx.esc(a.webhook_url ?? '')}" placeholder="https://your-app.com/orpay/webhook"></label>
        <label>Settlement wallet (receives payments)<input data-field="settlement_address" class="mono" value="${ctx.esc(a.settlement_address)}"></label>
        <label>Website <span class="muted">(your logo is taken from here automatically)</span><input data-field="website" value="${ctx.esc(a.website)}" placeholder="https://gabis.pages.dev"></label>
        <div class="logo-row">
          ${a.logo_type ? partnerMark({ ...a, status: 'approved', logo_url: `/api/apps/${a.id}/logo?v=${Date.parse(a.logo_at) || 0}` }, 'pool-avatar') : '<span class="pool-avatar muted-bg">?</span>'}
          <span class="small-text muted">${a.logo_type ? `Logo from <span class="mono">${ctx.esc(new URL(a.logo_source).host)}</span>, refreshed daily` : 'No logo found yet on your website.'}</span>
          <button class="ghost small" data-act="logo">Refresh logo</button>
        </div>
        <div class="brand-edit">
          <label>Brand name shown beside ORPay<input data-field="brand_name" maxlength="40" value="${ctx.esc(a.brand_name ?? '')}" placeholder="${ctx.esc(a.name)}"></label>
          <label>Brand colour<input data-field="brand_color" type="color" value="${ctx.esc(a.brand_color || '#4f46e5')}"></label>
        </div>
        <div class="brand-preview" data-preview><span class="logo cobrand">${a.logo_type ? partnerMark({ ...a, status: 'approved', logo_url: `/api/apps/${a.id}/logo?v=${Date.parse(a.logo_at) || 0}` }) : `<span class="partner-mark">${ctx.esc((a.brand_name || a.name).slice(0, 1).toUpperCase())}</span>`}<span class="partner-name">${ctx.esc(a.brand_name || a.name)}</span><span class="cobrand-x">×</span><span class="cobrand-orpay">ORPay</span></span></div>
        <label>Escrow terms buyers must accept <span class="muted">(returns are not supported)</span><textarea data-field="escrow_policy" rows="3" maxlength="4000" placeholder="No returns. Inspect the item on delivery. Disputes only for items not as described.">${ctx.esc(a.escrow_policy ?? '')}</textarea></label>
        <label>Escrow arbiter (settles disputes; defaults to your wallet)<input data-field="arbiter_address" class="mono" value="${ctx.esc(a.arbiter_address ?? '')}" placeholder="${ctx.esc(a.owner)}"></label>
        <button class="ghost small" data-act="save">Save settings</button>
        <details>
          <summary>Webhook signing secret</summary>
          <div class="secret mono">${ctx.esc(a.webhook_secret ?? '')}</div>
        </details>
        <details>
          <summary>Quick start</summary>
          <pre class="code">${ctx.esc(snippet(a))}</pre>
        </details>
      </div>` : ''}
    </section>`
}

function snippet() {
  const base = location.origin
  return `# 1. Create a checkout from your server
curl -X POST ${base}/api/v1/checkout \\
  -H "Authorization: Bearer $ORPAY_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"amount":"12.50","description":"Order #42","reference":"order-42",
       "return_url":"https://your-app.com/orders/42"}'

# 2. Redirect the customer to the returned checkout_url.

# 3. Verify webhooks: header ORPay-Signature: t=<ts>,v1=<hex>
#    v1 = HMAC-SHA256(webhook_secret, "<ts>." + raw_body)
#    Events: invoice.paid, invoice.expired

# Escrow (e.g. hold a developer's payout until work is approved)
curl -X POST ${base}/api/v1/escrows \\
  -H "Authorization: Bearer $ORPAY_API_KEY" -H "Content-Type: application/json" \\
  -d '{"seller":"@developer","currency":"NGN","description":"Landing page",
       "milestones":[{"label":"Design approved","amount":"40000"},
                     {"label":"Site delivered","amount":"60000"}],
       "ship_by_days":14,"review_days":3,"reference":"job-17"}'
# Send the client to funding_url. Events: escrow.funded, escrow.dispatched,
# escrow.milestone_released, escrow.disputed, escrow.completed,
# escrow.refunded, escrow.resolved, escrow.expired`
}

function bindAppCards(apps) {
  for (const a of apps) {
    const card = ctx.$(`[data-app="${a.id}"]`)
    if (!card) continue
    const keyBtn = card.querySelector('[data-act="key"]')
    if (keyBtn)
      keyBtn.onclick = async () => {
        if (a.key_prefix && !confirm('Rolling the key stops the old key working immediately. Continue?')) return
        keyBtn.disabled = true
        try {
          const r = await api.rotateKey(ctx.state.seed, a.id)
          const out = card.querySelector('.key-out')
          out.hidden = false
          out.innerHTML = `<div class="secret mono">${ctx.esc(r.api_key)}</div><p class="banner warn">Copy this key now. It will not be shown again.</p>`
          navigator.clipboard?.writeText(r.api_key).then(() => ctx.toast('API key copied')).catch(() => {})
        } catch (err) {
          ctx.toast(err.message, 'err')
        }
        keyBtn.disabled = false
      }
    // Live preview of the co-branded header.
    const preview = card.querySelector('[data-preview]')
    const nameIn = card.querySelector('[data-field="brand_name"]')
    const colorIn = card.querySelector('[data-field="brand_color"]')
    if (preview && nameIn && colorIn) {
      const update = () => {
        const n = nameIn.value.trim() || a.name
        preview.querySelector('.partner-name').textContent = n
        const mark = preview.querySelector('span.partner-mark')
        if (mark) mark.textContent = n.slice(0, 1).toUpperCase()
        preview.style.setProperty('--partner', colorIn.value)
      }
      nameIn.oninput = update
      colorIn.oninput = update
      update()
    }
    const logoBtn = card.querySelector('[data-act="logo"]')
    if (logoBtn)
      logoBtn.onclick = async () => {
        logoBtn.disabled = true
        logoBtn.textContent = 'Fetching…'
        try {
          await api.refreshLogo(ctx.state.seed, a.id)
          ctx.toast('Logo updated from your website')
          renderDevelopers()
        } catch (err) {
          ctx.toast(err.message, 'err')
          logoBtn.disabled = false
          logoBtn.textContent = 'Refresh logo'
        }
      }
    const save = card.querySelector('[data-act="save"]')
    if (save)
      save.onclick = async () => {
        const webhook_url = card.querySelector('[data-field="webhook_url"]').value.trim()
        const settlement_address = card.querySelector('[data-field="settlement_address"]').value.trim()
        const arbiter_address = card.querySelector('[data-field="arbiter_address"]').value.trim()
        if (!isAddress(settlement_address)) return ctx.toast('Settlement wallet must be a 64-character address', 'err')
        if (arbiter_address && !isAddress(arbiter_address)) return ctx.toast('Arbiter must be a 64-character address', 'err')
        save.disabled = true
        try {
          const website = card.querySelector('[data-field="website"]').value.trim()
          const escrow_policy = card.querySelector('[data-field="escrow_policy"]').value.trim()
          const brand_name = card.querySelector('[data-field="brand_name"]').value.trim()
          const brand_color = card.querySelector('[data-field="brand_color"]').value
          await api.updateApp(ctx.state.seed, a.id, { webhook_url, settlement_address, arbiter_address, brand_name, brand_color, website, escrow_policy })
          ctx.toast('Settings saved')
        } catch (err) {
          ctx.toast(err.message, 'err')
        }
        save.disabled = false
      }
  }
}

function renderRequest() {
  const root = ctx.root()
  root.innerHTML = `
    <section class="card">
      <button class="link back" id="back">← Developers</button>
      <h2>Request access</h2>
      <p class="muted small-text">Tell us about your company. An OpenReserve admin reviews each request before API keys are issued.</p>
      <form id="f" class="stack" autocomplete="off">
        <label>App or company name<input id="name" maxlength="40" placeholder="Gabis" required></label>
        <label>Website<input id="website" type="url" placeholder="https://gabis.app" required></label>
        <label>Contact email<input id="email" type="email" placeholder="dev@gabis.app" required></label>
        <label>What will you use ORPay for?<textarea id="desc" rows="3" maxlength="1000" placeholder="e.g. Customers pay invoices through Gabis Payments"></textarea></label>
        <label>Settlement wallet <span class="muted">(optional, defaults to this wallet)</span><input id="settle" class="mono" placeholder="64-character address"></label>
        <p class="error" id="err"></p>
        <button class="primary" id="go">Submit request</button>
      </form>
    </section>`
  ctx.$('#back').onclick = renderDevelopers
  ctx.$('#f').onsubmit = async (e) => {
    e.preventDefault()
    const btn = ctx.$('#go')
    btn.disabled = true
    try {
      const body = {
        name: ctx.$('#name').value.trim(),
        website: ctx.$('#website').value.trim(),
        contact_email: ctx.$('#email').value.trim(),
        description: ctx.$('#desc').value.trim(),
      }
      const settle = ctx.$('#settle').value.trim()
      if (settle) body.settlement_address = settle
      await api.requestApp(ctx.state.seed, body)
      ctx.toast('Request sent for review')
      renderDevelopers()
    } catch (err) {
      ctx.$('#err').textContent = err.message
      btn.disabled = false
    }
  }
}
