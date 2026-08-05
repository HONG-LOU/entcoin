package entpay

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="color-scheme" content="light">
  <title>EntPay Merchant Workspace</title>
  <link rel="icon" href="favicon.ico">
  <link rel="stylesheet" href="style.css">
</head>
<body>
  <div class="shell">
    <header class="topbar">
      <a class="brand" href="./" aria-label="EntPay home"><span class="brand-mark">E</span><span>EntPay</span></a>
      <div class="network"><span class="pulse"></span><span id="network">CONNECTING</span></div>
      <div class="merchant"><span>MERCHANT</span><strong id="merchant">--</strong></div>
    </header>

    <main class="workspace">
      <aside class="catalog">
        <div class="catalog-head"><p>MARKETPLACE</p><strong id="product-count">0 services</strong></div>
        <nav id="products" aria-label="Available services"></nav>
        <div class="trust-note"><span>VERIFIED FLOW</span><p>Signed invoice · Confirmed payment · Signed receipt</p></div>
      </aside>

      <section class="composer">
        <div class="composer-head">
          <div><p id="product-id">SELECT A SERVICE</p><h1 id="product-name">Agent commerce, settled in ENT.</h1></div>
          <span id="price" class="price">-- ENT</span>
        </div>
        <p id="description" class="description">Choose a merchant service to prepare a cryptographically bound invoice.</p>
        <form id="invoice-form">
          <div id="fields" class="fields"></div>
          <div class="terms">
            <div><span>PRICE</span><strong id="term-price">--</strong></div>
            <div><span>FINALITY</span><strong id="term-confirmations">--</strong></div>
            <div><span>PROTOCOL</span><strong id="protocol">--</strong></div>
          </div>
          <button id="create" class="primary" type="submit" disabled><span>Create signed invoice</span><span aria-hidden="true">-&gt;</span></button>
        </form>
      </section>

      <aside class="proof">
        <div class="proof-head"><span>PAYMENT OBJECT</span><span id="stage" class="stage">READY</span></div>
        <div id="empty" class="empty-state"><span class="seal">E</span><h2>No invoice yet</h2><p>Terms and input will be hashed, signed and returned here.</p></div>
        <div id="invoice" class="invoice" hidden>
          <div class="invoice-total"><span>AMOUNT DUE</span><strong id="invoice-amount">--</strong></div>
          <dl>
            <div><dt>Invoice</dt><dd id="invoice-id">--</dd></div>
            <div><dt>Resource</dt><dd id="invoice-resource">--</dd></div>
            <div><dt>Expires</dt><dd id="invoice-expires">--</dd></div>
            <div><dt>Input hash</dt><dd id="invoice-hash">--</dd></div>
            <div><dt>Signature</dt><dd id="invoice-signature">--</dd></div>
          </dl>
          <button id="copy" class="secondary" type="button">Copy Agent payload</button>
          <p class="handoff">Hand this payload to an EntPay Agent. Wallet keys never enter the merchant service.</p>
        </div>
        <div id="notice" class="notice" role="status" aria-live="polite"></div>
      </aside>
    </main>

    <footer><span>ENTROPY MAINNET</span><span>Invoice - confirmation - fulfillment - receipt</span><span id="capabilities">0 capabilities</span></footer>
  </div>
  <script src="app.js"></script>
</body>
</html>`

const appJavaScript = `const $ = (id) => document.getElementById(id);
const state = {info: null, product: null, payload: null};
const colors = {green: "#16865f", coral: "#e0644b", blue: "#3179ba", gold: "#b98219"};
const amount = (atoms) => (Number(atoms) / 100000000).toFixed(8) + " ENT";
const compact = (value, size = 15) => value && value.length > size * 2 ? value.slice(0, size) + "..." + value.slice(-size) : value;

function notice(message, error = false) {
  $("notice").textContent = message;
  $("notice").classList.toggle("error", error);
}

function selectProduct(product) {
  state.product = product;
  document.documentElement.style.setProperty("--accent", colors[product.accent] || colors.green);
  document.querySelectorAll(".product").forEach((item) => item.classList.toggle("active", item.dataset.id === product.id));
  $("product-id").textContent = product.id.toUpperCase();
  $("product-name").textContent = product.name;
  $("description").textContent = product.description;
  $("price").textContent = amount(product.price);
  $("term-price").textContent = amount(product.price);
  $("term-confirmations").textContent = product.confirmations + (product.confirmations === 1 ? " block" : " blocks");
  $("fields").replaceChildren(...product.fields.map(fieldControl));
  $("create").disabled = false;
  notice("");
}

function fieldControl(field) {
  const wrapper = document.createElement("label");
  wrapper.className = "field";
  const title = document.createElement("span");
  title.textContent = field.label;
  const control = document.createElement(field.type === "textarea" ? "textarea" : "input");
  control.name = field.id;
  control.placeholder = field.placeholder || "";
  control.value = field.default || "";
  control.required = Boolean(field.required);
  if (field.min_length) control.minLength = field.min_length;
  if (field.max_length) control.maxLength = field.max_length;
  if (control.tagName === "INPUT") control.type = "text";
  wrapper.append(title, control);
  return wrapper;
}

function productButton(product, index) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "product";
  button.dataset.id = product.id;
  const number = document.createElement("span");
  number.className = "product-number";
  number.textContent = String(index + 1).padStart(2, "0");
  const copy = document.createElement("span");
  copy.className = "product-copy";
  const name = document.createElement("strong");
  name.textContent = product.name;
  const price = document.createElement("small");
  price.textContent = amount(product.price);
  copy.append(name, price);
  const arrow = document.createElement("span");
  arrow.className = "product-arrow";
  arrow.textContent = ">";
  button.append(number, copy, arrow);
  button.addEventListener("click", () => selectProduct(product));
  return button;
}

async function loadInfo() {
  const response = await fetch("v1/info", {headers: {Accept: "application/json"}});
  if (!response.ok) throw new Error("Merchant service unavailable");
  state.info = await response.json();
  $("network").textContent = state.info.network.toUpperCase();
  $("merchant").textContent = compact(state.info.merchant, 10);
  $("merchant").title = state.info.merchant;
  $("protocol").textContent = state.info.protocol;
  $("product-count").textContent = state.info.products.length + (state.info.products.length === 1 ? " service" : " services");
  $("capabilities").textContent = state.info.capabilities.length + " capabilities";
  $("products").replaceChildren(...state.info.products.map(productButton));
  if (state.info.products.length) selectProduct(state.info.products[0]);
}

$("invoice-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!state.product) return;
  const button = $("create");
  const input = Object.fromEntries(new FormData(event.currentTarget).entries());
  button.disabled = true;
  $("stage").textContent = "SIGNING";
  notice("Creating a signed, input-bound invoice...");
  try {
    const response = await fetch("v1/invoices", {method: "POST", headers: {"Content-Type": "application/json", Accept: "application/json"}, body: JSON.stringify({resource: state.product.id, input})});
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Invoice could not be created");
    state.payload = result;
    $("empty").hidden = true;
    $("invoice").hidden = false;
    $("invoice-amount").textContent = amount(result.invoice.amount);
    $("invoice-id").textContent = compact(result.invoice.id);
    $("invoice-id").title = result.invoice.id;
    $("invoice-resource").textContent = result.invoice.resource;
    $("invoice-expires").textContent = new Date(result.invoice.expires_at).toLocaleString();
    $("invoice-hash").textContent = compact(result.invoice.input_sha256);
    $("invoice-hash").title = result.invoice.input_sha256;
    $("invoice-signature").textContent = compact(result.invoice.signature);
    $("invoice-signature").title = result.invoice.signature;
    $("stage").textContent = "OPEN";
    notice("Invoice ready for a local EntPay Agent.");
  } catch (error) {
    $("stage").textContent = "ERROR";
    notice(error.message, true);
  } finally {
    button.disabled = false;
  }
});

$("copy").addEventListener("click", async () => {
  if (!state.payload) return;
  try {
    await navigator.clipboard.writeText(JSON.stringify(state.payload, null, 2));
    notice("Agent payload copied.");
  } catch (_) {
    notice("Clipboard permission was denied.", true);
  }
});

loadInfo().catch((error) => { $("network").textContent = "OFFLINE"; notice(error.message, true); });`

const styleCSS = `:root{color-scheme:light;--paper:#f4f3ee;--ink:#18201d;--muted:#66706a;--line:#d8d9d2;--panel:#fbfbf8;--accent:#16865f;--green:#16865f;--coral:#e0644b;--blue:#3179ba;--gold:#b98219;letter-spacing:0}*{box-sizing:border-box}html,body{margin:0;min-width:320px;min-height:100%;background:var(--paper);color:var(--ink);font:14px/1.5 Arial,"Noto Sans SC",sans-serif;letter-spacing:0}button,input,textarea{font:inherit;letter-spacing:0}[hidden]{display:none!important}.shell{min-height:100dvh;display:grid;grid-template-rows:72px minmax(0,1fr) 40px}.topbar{display:grid;grid-template-columns:260px 1fr minmax(240px,360px);align-items:center;border-bottom:1px solid var(--line);background:var(--panel)}.brand{height:100%;display:flex;align-items:center;gap:12px;padding:0 24px;color:var(--ink);text-decoration:none;font-size:18px;font-weight:800;border-right:1px solid var(--line)}.brand-mark,.seal{display:grid;place-items:center;background:var(--ink);color:#fff;font:800 16px/1 Georgia,serif;width:30px;height:30px}.network{justify-self:center;display:flex;align-items:center;gap:9px;color:var(--muted);font-size:11px;font-weight:800}.pulse{width:7px;height:7px;border-radius:50%;background:var(--green);box-shadow:0 0 0 4px #dcece5}.merchant{height:100%;padding:0 24px;border-left:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;gap:18px;min-width:0}.merchant span,.catalog-head p,.composer-head p,.proof-head,.terms span,.invoice-total span,footer{font-size:10px;font-weight:800;color:var(--muted)}.merchant strong{font:600 12px/1.4 ui-monospace,SFMono-Regular,Consolas,monospace;overflow:hidden;text-overflow:ellipsis}.workspace{display:grid;grid-template-columns:260px minmax(420px,1fr) minmax(320px,400px);min-height:0}.catalog{display:flex;flex-direction:column;border-right:1px solid var(--line);background:#eceee8}.catalog-head{padding:27px 24px 22px}.catalog-head p{margin:0 0 4px}.catalog-head strong{font-size:19px}#products{border-top:1px solid var(--line)}.product{width:100%;min-height:92px;padding:16px 20px;border:0;border-bottom:1px solid var(--line);background:transparent;color:var(--ink);display:grid;grid-template-columns:26px 1fr 20px;gap:10px;align-items:center;text-align:left;cursor:pointer}.product:hover{background:#f5f6f1}.product.active{background:var(--panel);box-shadow:inset 4px 0 var(--accent)}.product-number,.product-arrow{color:var(--muted);font-size:11px}.product-arrow{font-size:16px}.product-copy{min-width:0}.product-copy strong,.product-copy small{display:block}.product-copy strong{font-size:14px;white-space:normal}.product-copy small{margin-top:5px;color:var(--muted);font-size:11px}.trust-note{margin:auto 20px 20px;padding:17px;border-top:2px solid var(--ink);background:#dfe4dc}.trust-note span{font-size:10px;font-weight:800}.trust-note p{margin:7px 0 0;color:var(--muted);font-size:11px}.composer{padding:clamp(28px,5vw,72px);overflow:auto;background:var(--panel)}.composer-head{display:flex;align-items:flex-start;justify-content:space-between;gap:32px}.composer-head p{margin:0 0 12px;color:var(--accent)}.composer-head h1{margin:0;max-width:650px;font:500 48px/1.05 Georgia,"Noto Serif SC",serif;letter-spacing:0}.price{flex:none;border:1px solid var(--line);padding:8px 11px;color:var(--accent);font:700 12px ui-monospace,monospace}.description{max-width:690px;margin:24px 0 42px;color:var(--muted);font-size:15px}.fields{display:grid;gap:22px}.field>span{display:block;margin-bottom:8px;font-size:12px;font-weight:700}.field input,.field textarea{display:block;width:100%;border:1px solid var(--line);border-radius:2px;outline:0;background:#fff;color:var(--ink);padding:14px 15px}.field textarea{min-height:150px;resize:vertical}.field input:focus,.field textarea:focus{border-color:var(--accent);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent) 12%,transparent)}.terms{display:grid;grid-template-columns:repeat(3,1fr);margin-top:32px;border:1px solid var(--line)}.terms div{padding:14px 16px;border-right:1px solid var(--line);min-width:0}.terms div:last-child{border:0}.terms span,.terms strong{display:block}.terms strong{margin-top:5px;font:700 12px ui-monospace,monospace;overflow-wrap:anywhere}.primary,.secondary{border:0;border-radius:2px;cursor:pointer;font-weight:800}.primary{width:100%;height:52px;margin-top:16px;padding:0 18px;display:flex;align-items:center;justify-content:space-between;background:var(--accent);color:#fff}.primary:disabled{opacity:.45;cursor:wait}.proof{position:relative;border-left:1px solid var(--line);background:#e9ebe5;padding:24px;overflow:auto}.proof-head{display:flex;justify-content:space-between;align-items:center;padding-bottom:18px;border-bottom:1px solid var(--line)}.stage{color:var(--accent)}.empty-state{min-height:390px;display:flex;flex-direction:column;align-items:center;justify-content:center;text-align:center}.empty-state .seal{width:48px;height:48px;background:transparent;color:var(--muted);border:1px solid #aeb4ac;font-size:22px}.empty-state h2{margin:20px 0 5px;font:500 23px Georgia,serif}.empty-state p{max-width:240px;margin:0;color:var(--muted);font-size:12px}.invoice-total{margin:24px 0 14px;padding:22px;background:var(--ink);color:#fff}.invoice-total span,.invoice-total strong{display:block}.invoice-total span{color:#aeb8b2}.invoice-total strong{margin-top:7px;font:500 25px Georgia,serif}.invoice dl{margin:0}.invoice dl div{display:grid;grid-template-columns:84px minmax(0,1fr);gap:10px;padding:11px 2px;border-bottom:1px solid var(--line)}.invoice dt{color:var(--muted);font-size:11px}.invoice dd{margin:0;font:600 11px/1.5 ui-monospace,monospace;overflow-wrap:anywhere}.secondary{width:100%;height:42px;margin-top:18px;border:1px solid var(--ink);background:transparent;color:var(--ink)}.secondary:hover{background:var(--ink);color:#fff}.handoff{color:var(--muted);font-size:11px}.notice{min-height:20px;margin-top:15px;color:var(--green);font-size:11px}.notice.error{color:#b13c35}footer{display:grid;grid-template-columns:260px 1fr 400px;align-items:center;border-top:1px solid var(--line);background:var(--panel)}footer span{padding:0 24px}footer span:nth-child(2){text-align:center}footer span:last-child{text-align:right}
:root{--paper:#eef3f6;--ink:#121b22;--muted:#60717b;--line:#ccd5da;--panel:#f8fafb;--coral:#f06449;--yellow:#f2cb45}.topbar{background:var(--ink);color:#fff;border-color:#354650}.brand{color:#fff;border-color:#354650}.brand-mark{background:var(--coral)}.network{color:#c6d2d8}.pulse{background:#31d096;box-shadow:0 0 0 4px #243e37}.merchant{border-color:#354650}.merchant span{color:#9fb0b9}.catalog{background:#17242c;color:#fff;border-color:#354650}.catalog-head p{color:#8fa2ac}.catalog-head strong{color:#fff}#products{border-color:#354650}.product{color:#fff;border-color:#354650}.product:hover{background:#22333d}.product.active{background:var(--accent);box-shadow:inset 5px 0 var(--yellow);color:#fff}.product-number,.product-arrow,.product-copy small{color:#9fb0b9}.product.active .product-number,.product.active .product-arrow,.product.active .product-copy small{color:#e7f1f7}.trust-note{background:var(--yellow);color:var(--ink);border-color:var(--coral)}.trust-note p{color:#5d522c}.composer{background:var(--panel)}.composer-head h1{font-family:Arial,"Noto Sans SC",sans-serif;font-weight:800;line-height:1.08}.price{background:var(--accent);border-color:var(--accent);color:#fff}.proof{background:var(--yellow);border-color:#bca239}.proof-head{border-color:#bca239;color:#66581f}.stage{color:#145dab}.empty-state .seal{border-color:#7d6b24;color:#6b5b21}.empty-state h2{font-family:Arial,"Noto Sans SC",sans-serif;font-weight:800}.invoice-total{background:var(--ink)}.invoice dl div{border-color:#bca239}.secondary{border-color:var(--ink)}.notice{color:#145dab}footer{background:var(--ink);color:#aebcc3;border-color:#354650}
@media(max-width:1000px){.topbar{grid-template-columns:220px 1fr 260px}.workspace{grid-template-columns:220px minmax(380px,1fr) 330px}.composer{padding:38px 30px}.composer-head{display:block}.price{display:inline-block;margin-top:18px}footer{grid-template-columns:220px 1fr 330px}}
@media(max-width:780px){.shell{display:block;min-height:100dvh}.topbar{height:62px;grid-template-columns:1fr auto}.brand{border-right:0;padding:0 17px}.network{padding-right:17px}.merchant{display:none}.workspace{display:block}.catalog{border-right:0}.catalog-head{padding:22px 18px 14px}#products{display:flex;overflow-x:auto}.product{flex:0 0 210px;min-height:78px;border-right:1px solid var(--line)}.trust-note{display:none}.composer{padding:34px 18px}.composer-head h1{font-size:34px}.description{margin:18px 0 30px}.terms{grid-template-columns:1fr}.terms div{border-right:0;border-bottom:1px solid var(--line)}.terms div:last-child{border-bottom:0}.proof{border-left:0;border-top:1px solid var(--line);padding:24px 18px;min-height:460px}.empty-state{min-height:330px}footer{min-height:78px;display:flex;flex-wrap:wrap;gap:6px;padding:15px 17px}footer span{padding:0}footer span:nth-child(2){text-align:left}footer span:last-child{margin-left:auto}}
@media(max-width:390px){.composer-head h1{font-size:30px}.product{flex-basis:190px}.invoice dl div{grid-template-columns:72px minmax(0,1fr)}}`
