package entpay

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>EntPay Agent</title>
  <link rel="icon" href="favicon.ico">
  <link rel="stylesheet" href="style.css">
</head>
<body>
  <header>
    <a class="brand" href="https://entcoin.xyz"><img src="https://entcoin.xyz/assets/appicon.png" alt="Entcoin"><span>EntPay Agent</span></a>
    <span id="health" class="health">连接中</span>
  </header>
  <main>
    <section class="summary">
      <div><span>协议</span><strong id="protocol">--</strong></div>
      <div><span>资源</span><strong id="resource">--</strong></div>
      <div><span>价格</span><strong id="price">--</strong></div>
      <div><span>确认</span><strong id="confirmations">--</strong></div>
    </section>
    <section class="workspace">
      <div class="panel request-panel">
        <div class="panel-title"><span>01</span><h1>创建 Agent 支付请求</h1></div>
        <label for="query">分析目标</label>
        <textarea id="query" maxlength="500">检查两个 Entcoin 公网节点是否处于同一条主链，并总结当前网络状态。</textarea>
        <button id="create" type="button">创建 Invoice</button>
      </div>
      <div class="panel invoice-panel">
        <div class="panel-title"><span>02</span><h2>Invoice</h2></div>
        <div id="empty" class="empty">等待新请求</div>
        <dl id="invoice" hidden>
          <dt>Invoice ID</dt><dd id="invoice-id"></dd>
          <dt>收款地址</dt><dd id="merchant"></dd>
          <dt>金额</dt><dd id="amount"></dd>
          <dt>过期时间</dt><dd id="expires"></dd>
          <dt>状态</dt><dd><span id="invoice-status" class="status">OPEN</span></dd>
        </dl>
      </div>
    </section>
    <section class="nodes panel">
      <div class="panel-title"><span>LIVE</span><h2>验证节点</h2></div>
      <div id="nodes" class="node-grid"></div>
    </section>
  </main>
  <footer><span>ENTROPY-MAINNET-V1</span><span>Private keys stay with the local agent wallet</span></footer>
  <script src="app.js"></script>
</body>
</html>`

const appJavaScript = `const byId = (id) => document.getElementById(id);
const atoms = (value) => (Number(value) / 100000000).toFixed(8) + " ENT";

async function loadInfo() {
  const response = await fetch("v1/info");
  const info = await response.json();
  byId("protocol").textContent = info.protocol;
  byId("resource").textContent = info.resource;
  byId("price").textContent = atoms(info.price);
  byId("confirmations").textContent = info.confirmations + " block";
  byId("health").textContent = "ONLINE";
  byId("health").classList.add("online");
  byId("nodes").replaceChildren(...info.nodes.map((url, index) => {
    const item = document.createElement("div");
    item.className = "node";
    const mark = document.createElement("span");
    mark.textContent = "0" + (index + 1);
    const value = document.createElement("strong");
    value.textContent = new URL(url).host;
    item.append(mark, value);
    return item;
  }));
}

byId("create").addEventListener("click", async () => {
  const button = byId("create");
  button.disabled = true;
  try {
    const response = await fetch("v1/invoices", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({resource: "network-report", query: byId("query").value})
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Invoice failed");
    byId("empty").hidden = true;
    byId("invoice").hidden = false;
    byId("invoice-id").textContent = result.invoice.id;
    byId("merchant").textContent = result.invoice.merchant;
    byId("amount").textContent = atoms(result.invoice.amount);
    byId("expires").textContent = new Date(result.invoice.expires_at).toLocaleString();
    byId("invoice-status").textContent = "OPEN";
  } catch (error) {
    byId("health").textContent = error.message;
    byId("health").classList.remove("online");
  } finally {
    button.disabled = false;
  }
});

loadInfo().catch(() => { byId("health").textContent = "OFFLINE"; });`

const styleCSS = `:root{color-scheme:dark;--bg:#0b0e11;--surface:#14181d;--line:#2a3037;--text:#f3f5f7;--muted:#929ba5;--green:#49d17d;--amber:#f2b84b;--red:#ef6461}*{box-sizing:border-box}[hidden]{display:none!important}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 Inter,Segoe UI,Arial,sans-serif;letter-spacing:0}header{height:64px;border-bottom:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;padding:0 max(20px,calc((100vw - 1180px)/2))}.brand{display:flex;align-items:center;gap:11px;color:var(--text);text-decoration:none;font-weight:700;font-size:17px}.brand img{width:30px;height:30px}.health{border:1px solid var(--line);padding:5px 9px;font:700 11px/1 monospace;color:var(--muted)}.health.online{border-color:#245d3a;color:var(--green)}main{max-width:1180px;margin:0 auto;padding:34px 20px 48px}.summary{display:grid;grid-template-columns:repeat(4,1fr);border:1px solid var(--line);margin-bottom:18px}.summary div{padding:18px 20px;border-right:1px solid var(--line)}.summary div:last-child{border:0}.summary span{display:block;color:var(--muted);font-size:11px;text-transform:uppercase;margin-bottom:7px}.summary strong{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:15px}.workspace{display:grid;grid-template-columns:1.06fr .94fr;gap:18px}.panel{background:var(--surface);border:1px solid var(--line);padding:24px}.panel-title{display:flex;align-items:center;gap:12px;margin-bottom:24px}.panel-title span{color:var(--amber);font:700 11px/1 ui-monospace,monospace}.panel-title h1,.panel-title h2{font-size:17px;line-height:1.2;margin:0;letter-spacing:0}label{display:block;color:var(--muted);font-size:12px;margin-bottom:8px}textarea{width:100%;height:138px;resize:vertical;background:#0d1115;border:1px solid var(--line);color:var(--text);padding:14px;font:14px/1.55 inherit;outline:none;border-radius:3px}textarea:focus{border-color:#687581}button{margin-top:14px;width:100%;height:42px;border:0;border-radius:3px;background:var(--green);color:#07130c;font-weight:800;cursor:pointer}button:disabled{opacity:.55;cursor:wait}.empty{height:196px;display:grid;place-items:center;border:1px dashed var(--line);color:var(--muted)}dl{display:grid;grid-template-columns:100px minmax(0,1fr);margin:0;gap:0}dt,dd{margin:0;padding:11px 0;border-bottom:1px solid var(--line)}dt{color:var(--muted);font-size:12px}dd{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;overflow-wrap:anywhere}.status{display:inline-block;color:var(--amber);border:1px solid #66501d;padding:3px 7px;font-size:11px}.nodes{margin-top:18px}.node-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}.node{border:1px solid var(--line);padding:14px 16px;display:flex;gap:14px;align-items:center}.node span{color:var(--green);font:700 11px ui-monospace,monospace}.node strong{font:600 13px ui-monospace,monospace;overflow-wrap:anywhere}footer{border-top:1px solid var(--line);padding:20px max(20px,calc((100vw - 1180px)/2));display:flex;justify-content:space-between;color:var(--muted);font:11px ui-monospace,monospace}@media(max-width:760px){header{height:58px}.summary{grid-template-columns:repeat(2,1fr)}.summary div:nth-child(2){border-right:0}.summary div:nth-child(-n+2){border-bottom:1px solid var(--line)}.workspace{grid-template-columns:1fr}.node-grid{grid-template-columns:1fr}.panel{padding:18px}footer{display:block}footer span{display:block;margin-bottom:6px}}`
