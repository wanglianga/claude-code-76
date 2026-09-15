/* 社区灭蚊消杀预约与积水点复查服务 - 前端 */
let token = localStorage.getItem('token') || '';
let me = JSON.parse(localStorage.getItem('me') || 'null');
let meta = null;
let currentTab = '';
let chemCache = null;

/* ---------- 基础工具 ---------- */
async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || 'GET',
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 401) { doLogout(); throw new Error(data.message || '登录已过期'); }
  if (!res.ok) throw new Error(data.message || ('请求失败 ' + res.status));
  return data.data;
}
function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
function fmtT(t) { if (!t) return '-'; const d = new Date(t); return d.toLocaleString('zh-CN', { hour12: false }); }
function val(id) { const el = document.getElementById(id); return el ? el.value.trim() : ''; }
function chk(id) { const el = document.getElementById(id); return el ? el.checked : false; }
function num(id) { const v = parseFloat(val(id)); return isNaN(v) ? 0 : v; }
function toast(msg, ok) {
  const el = document.getElementById('msg-bar');
  if (el) { el.className = 'msg ' + (ok === false ? 'err' : 'ok'); el.textContent = msg; el.style.display = 'block'; setTimeout(() => { el.style.display = 'none'; }, 4000); }
  else alert(msg);
}
async function run(fn) { try { await fn(); } catch (e) { toast(e.message, false); } }

const STATUS_COLOR = {
  pending: 'b-orange', assigned: 'b-blue', in_progress: 'b-blue', recheck_pending: 'b-purple',
  rectifying: 'b-orange', escalated: 'b-red', closed: 'b-green', cleared: 'b-green', treating: 'b-blue',
  dispatched: 'b-blue', processing: 'b-blue', open: 'b-red', resolved: 'b-green',
  done: 'b-purple', verified: 'b-green', rejected: 'b-red',
};
function badge(text, color) { return `<span class="badge ${color || 'b-gray'}">${esc(text)}</span>`; }
function sBadge(status, label) { return badge(label || status, STATUS_COLOR[status]); }
function opts(map, selected) {
  return Object.entries(map).map(([k, v]) => `<option value="${k}" ${k === selected ? 'selected' : ''}>${esc(v)}</option>`).join('');
}
function commOpts(selected) {
  return (meta.communities || []).map(c => `<option value="${c.id}" ${c.id === selected ? 'selected' : ''}>${esc(c.name)}</option>`).join('');
}
function teamOpts(selected) {
  return (meta.teams || []).map(t => `<option value="${t.id}" ${t.id === selected ? 'selected' : ''}>${esc(t.name)}</option>`).join('');
}

/* ---------- 登录 ---------- */
function renderLogin() {
  document.getElementById('app').innerHTML = `
  <div class="login-wrap">
    <h1>社区灭蚊消杀预约与积水点复查服务</h1>
    <div class="sub">上报 · 派单 · 消杀 · 复查 · 整改 · 闭环 · 考核</div>
    <div id="msg-bar" style="display:none"></div>
    <input id="login-user" placeholder="用户名" autocomplete="username">
    <input id="login-pass" type="password" placeholder="密码" autocomplete="current-password">
    <button onclick="doLogin()">登 录</button>
    <div class="demo-accounts">
      <b>演示账号</b>（角色 / 用户名 / 密码）<br>
      居民 resident / Resident@123 ｜ 物业 property / Property@123<br>
      网格员 grid / Grid@123 ｜ 消杀 operator / Operator@123<br>
      街道 street / Street@123 ｜ 卫生监督 supervisor / Supervisor@123<br>
      园方联系人 kindergarten / Kindergarten@123
    </div>
  </div>`;
}
async function doLogin() {
  try {
    const data = await api('/api/auth/login', { method: 'POST', body: { username: val('login-user'), password: val('login-pass') } });
    token = data.token; me = data.user;
    localStorage.setItem('token', token);
    localStorage.setItem('me', JSON.stringify(me));
    await boot();
  } catch (e) { toast(e.message, false); }
}
async function doLogout() {
  try { await api('/api/auth/logout', { method: 'POST' }); } catch (e) {}
  token = ''; me = null;
  localStorage.removeItem('token'); localStorage.removeItem('me');
  renderLogin();
}

/* ---------- 主框架 ---------- */
const TABS = {
  resident: [
    ['report', '上报问题'], ['reports', '我的上报'], ['pets', '宠物投诉'], ['zones', '儿童活动区'], ['notices', '居民告知'],
  ],
  property: [
    ['report', '上报问题'], ['rects', '整改任务'], ['prects', '积水整改'], ['orders', '小区工单'], ['notices', '居民告知'],
  ],
  grid: [
    ['report', '上报问题'], ['points', '积水点管理'], ['prects', '积水整改'], ['orders', '工单与复查'], ['rainfall', '降雨记录'],
  ],
  operator: [
    ['orders', '我的工单'], ['prects', '积水整改'], ['zones', '错峰消杀'], ['points', '积水点'], ['chems', '药剂库存'],
  ],
  street: [
    ['dispatch', '派单中心'], ['orders', '工单总览'], ['prects', '积水整改'], ['points', '积水点'], ['keylist', '重点积水点'],
    ['chems', '药剂库存'], ['sched', '排班管理'], ['rainfall', '降雨记录'], ['risk', '重点风险期'],
    ['zones', '错峰消杀'], ['pets', '宠物投诉'], ['archive', '小区档案'], ['notices', '居民告知'], ['loop', '闭环看板'], ['assess', '考核看板'],
  ],
  supervisor: [
    ['orders', '工单总览'], ['prects', '积水整改'], ['keylist', '重点积水点'], ['zones', '错峰消杀'], ['pets', '宠物投诉'], ['archive', '小区档案'], ['loop', '闭环看板'], ['assess', '考核看板'], ['notices', '居民告知'],
  ],
  kindergarten: [
    ['zones', '消杀计划确认'], ['notices', '居民告知'],
  ],
};

async function boot() {
  meta = await api('/api/meta');
  renderApp();
}
function renderApp() {
  const tabs = TABS[me.role] || [];
  document.getElementById('app').innerHTML = `
    <div class="topbar">
      <div class="title">🦟 社区灭蚊消杀预约与积水点复查服务</div>
      <div class="user">
        <span class="role">${esc(me.role_label)}</span>
        <span>${esc(me.name)}${me.community_name ? ' · ' + esc(me.community_name) : ''}${me.team_name ? ' · ' + esc(me.team_name) : ''}</span>
        <button onclick="doLogout()">退出</button>
      </div>
    </div>
    <div class="navbar">${tabs.map(([id, name]) => `<div class="tab" data-tab="${id}" onclick="switchTab('${id}')">${name}</div>`).join('')}</div>
    <div class="main"><div id="msg-bar" style="display:none"></div><div id="content"></div></div>`;
  switchTab(tabs[0][0]);
}
function switchTab(id) {
  currentTab = id;
  document.querySelectorAll('.navbar .tab').forEach(t => t.classList.toggle('active', t.dataset.tab === id));
  const render = TAB_RENDERERS[id];
  if (render) run(() => render(document.getElementById('content')));
}

/* ---------- 上报 ---------- */
async function renderReport(el) {
  const fixedComm = me.community_id ? me.community_id : null;
  el.innerHTML = `
  <div class="card"><h3>蚊虫问题上报（记录位置、照片、时间、周边人群、是否有宠物）</h3>
    <div class="form-grid">
      <div class="form-item"><label>问题类型 *</label><select id="rp-type">${opts(meta.report_types)}</select></div>
      <div class="form-item"><label>小区 *</label><select id="rp-comm" ${fixedComm ? 'disabled' : ''}>${commOpts(fixedComm)}</select></div>
      <div class="form-item"><label>位置描述 *</label><input id="rp-loc" placeholder="如：3号楼绿化带旁"></div>
      <div class="form-item"><label>周边人群</label><select id="rp-pop">
        <option>老人儿童居多</option><option>上班族为主</option><option>人流密集</option><option>商业区</option><option>其他</option>
      </select></div>
      <div class="form-item"><label>纬度（选填）</label><input id="rp-lat" placeholder="如 31.2304"></div>
      <div class="form-item"><label>经度（选填）</label><input id="rp-lng" placeholder="如 121.4737"></div>
      <div class="form-item full"><label>照片链接（多个用英文逗号分隔）</label><input id="rp-photos" placeholder="https://example.com/photo1.jpg, https://example.com/photo2.jpg"></div>
      <div class="form-item full"><label>情况描述</label><textarea id="rp-desc" placeholder="蚊虫密度、积水情况、异味等"></textarea></div>
      <div class="form-item full"><div class="check-row"><label><input type="checkbox" id="rp-pets"> 周边有宠物</label></div></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="submitReport()">提交上报</button></div>
  </div>`;
}
async function submitReport() {
  await run(async () => {
    const photos = val('rp-photos') ? val('rp-photos').split(',').map(s => s.trim()).filter(Boolean) : [];
    const body = {
      community_id: parseInt(val('rp-comm')) || 0,
      type: val('rp-type'),
      location_desc: val('rp-loc'),
      nearby_population: val('rp-pop'),
      has_pets: chk('rp-pets'),
      description: val('rp-desc'),
      photos,
    };
    if (val('rp-lat')) body.latitude = parseFloat(val('rp-lat'));
    if (val('rp-lng')) body.longitude = parseFloat(val('rp-lng'));
    const r = await api('/api/reports', { method: 'POST', body });
    toast('上报成功，单号 ' + r.report_no);
    switchTab(me.role === 'resident' ? 'reports' : 'report');
    if (me.role !== 'resident') renderReport(document.getElementById('content'));
  });
}

/* ---------- 上报列表 ---------- */
async function renderReports(el) {
  const list = await api('/api/reports');
  el.innerHTML = `
  <div class="card"><h3>${me.role === 'resident' ? '我的上报' : '上报记录'}</h3><div class="table-wrap"><table>
    <tr><th>单号</th><th>类型</th><th>小区</th><th>位置</th><th>周边人群</th><th>宠物</th><th>照片</th><th>上报人</th><th>时间</th><th>状态</th></tr>
    ${list.map(r => `<tr>
      <td>${esc(r.report_no)}</td><td>${esc(r.type_label)}</td><td>${esc(r.community_name)}</td>
      <td>${esc(r.location_desc)}${r.description ? '<br><span style="color:#90a4ae">' + esc(r.description) + '</span>' : ''}</td>
      <td>${esc(r.nearby_population || '-')}</td><td>${r.has_pets ? badge('有宠物', 'b-orange') : '无'}</td>
      <td>${(r.photos || []).map(p => `<a href="${esc(p)}" target="_blank">图</a>`).join(' ') || '-'}</td>
      <td>${esc(r.reporter_name)}</td><td>${fmtT(r.created_at)}</td><td>${sBadge(r.status, r.status_label)}</td>
    </tr>`).join('') || '<tr><td colspan="10">暂无数据</td></tr>'}
  </table></div></div>`;
}

/* ---------- 积水点 ---------- */
async function renderPoints(el) {
  const list = await api('/api/water-points');
  const canAdd = ['grid', 'street', 'property'].includes(me.role);
  el.innerHTML = `
  ${canAdd ? `<div class="card"><h3>登记积水点</h3>
    <div class="form-grid">
      <div class="form-item"><label>小区</label><select id="wp-comm">${commOpts(me.community_id)}</select></div>
      <div class="form-item"><label>类型</label><select id="wp-type">${opts(meta.water_point_types)}</select></div>
      <div class="form-item"><label>位置</label><input id="wp-loc" placeholder="如：7号楼楼顶水箱"></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="addPoint()">登记</button></div>
  </div>` : ''}
  <div class="card"><h3>积水点清单（覆盖楼顶水箱、废旧轮胎、地下车库、绿化灌木、雨水井、建筑工地等来源）</h3><div class="table-wrap"><table>
    <tr><th>ID</th><th>小区</th><th>类型</th><th>位置</th><th>来源</th><th>幼虫</th><th>重点</th><th>状态</th><th>最近消杀</th><th>最近复查</th></tr>
    ${list.map(w => `<tr>
      <td>${w.id}</td><td>${esc(w.community_name)}</td><td>${esc(w.type_label)}</td><td>${esc(w.location_desc)}</td>
      <td>${esc(w.source)}</td><td>${w.larvae_found ? badge('有幼虫', 'b-red') : '无'}</td>
      <td>${w.is_key ? badge('重点', 'b-red') + '<br><span style="font-size:11px;color:#90a4ae">' + esc(w.key_reason) + '</span>' : '-'}</td>
      <td>${sBadge(w.status, w.status_label)}</td>
      <td>${fmtT(w.last_treated_at)}</td><td>${fmtT(w.last_recheck_at)}</td>
    </tr>`).join('') || '<tr><td colspan="10">暂无数据</td></tr>'}
  </table></div></div>`;
}
async function addPoint() {
  await run(async () => {
    await api('/api/water-points', { method: 'POST', body: { community_id: parseInt(val('wp-comm')), type: val('wp-type'), location_desc: val('wp-loc') } });
    toast('积水点已登记');
    switchTab('points');
  });
}

/* ---------- 重点积水点清单 ---------- */
async function renderKeyList(el) {
  const data = await api('/api/water-points/key-list');
  const rp = data.risk_period;
  el.innerHTML = `
  ${rp ? `<div class="banner danger">⚠️ 当前处于「${esc(rp.name)}」（${esc(rp.disease)}，${rp.start_date} ~ ${rp.end_date}）：复查间隔缩短为 <b>${rp.recheck_interval_days} 天</b>，以下重点积水点需加密复查。</div>`
       : `<div class="banner ok">当前无生效中的重点风险期。</div>`}
  <div class="card"><h3>重点积水点清单（自动生成：曾发现幼虫 / 重点类型 / 投诉密集小区）</h3>
  ${me.role === 'street' ? '<div class="btn-row" style="margin:0 0 12px"><button class="btn outline sm" onclick="refreshKey()">按规则重新标记</button></div>' : ''}
  <div class="table-wrap"><table>
    <tr><th>ID</th><th>小区</th><th>类型</th><th>位置</th><th>标记原因</th><th>幼虫</th><th>状态</th></tr>
    ${data.list.map(w => `<tr><td>${w.id}</td><td>${esc(w.community_name)}</td><td>${esc(w.type_label)}</td>
      <td>${esc(w.location_desc)}</td><td>${esc(w.key_reason)}</td>
      <td>${w.larvae_found ? badge('有幼虫', 'b-red') : '无'}</td><td>${sBadge(w.status, w.status_label)}</td></tr>`).join('')
      || '<tr><td colspan="7">暂无重点积水点</td></tr>'}
  </table></div></div>`;
}
async function refreshKey() { await run(async () => { const r = await api('/api/water-points/refresh-key', { method: 'POST' }); toast(r.message); switchTab('keylist'); }); }

/* ---------- 派单中心 ---------- */
async function renderDispatch(el) {
  const data = await api('/api/dispatch/preview');
  const rp = data.risk_period;
  const today = new Date().toISOString().slice(0, 10);
  el.innerHTML = `
  ${rp ? `<div class="banner danger">⚠️「${esc(rp.name)}」生效中，派单优先级 ×1.5，复查间隔 ${rp.recheck_interval_days} 天。</div>` : ''}
  ${(data.pet_tracking || []).filter(t => t.total_complaints > 0).map(t => `<div class="banner">🐾 ${esc(t.community_name)}：宠物投诉 ${t.total_complaints} 起（待处理 ${t.pending_complaints}）${t.latest_adjustment ? '，最近告知调整「' + esc(t.latest_adjustment) + '」' + (t.decreased ? '，同类投诉已下降' : (t.decreased === false ? '，同类投诉未下降，请关注' : '')) : ''}，请纳入本次计划参考。</div>`).join('')}
  <div class="card"><h3>待派单池（按 小区 / 积水点类型 / 近期降雨 / 投诉密度 综合评分）</h3>
    <div class="filter-row">
      <label>计划消杀日期 <input type="date" id="dp-date" value="${today}"></label>
      <button class="btn" onclick="genDispatch()">生成派单</button>
      <button class="btn outline" onclick="switchTab('dispatch')">刷新评分</button>
    </div>
    <div id="dp-result"></div>
    <div class="table-wrap"><table>
      <tr><th>#</th><th>来源</th><th>单号</th><th>小区</th><th>类型</th><th>位置</th><th>评分</th><th>评分依据</th></tr>
      ${data.candidates.map((c, i) => `<tr>
        <td>${i + 1}</td><td>${c.kind === 'report' ? badge('投诉上报', 'b-orange') : badge('积水点', 'b-blue')}</td>
        <td>${esc(c.ref_no)}</td><td>${esc(c.community_name)}</td><td>${esc(c.type_label)}</td>
        <td>${esc(c.location_desc)}</td><td><b>${c.score.toFixed(1)}</b></td>
        <td style="font-size:12px;color:#607d8b">${c.reasons.map(esc).join('<br>')}</td>
      </tr>`).join('') || '<tr><td colspan="8">待派单池为空</td></tr>'}
    </table></div>
  </div>`;
}
async function genDispatch() {
  await run(async () => {
    const r = await api('/api/dispatch/generate', { method: 'POST', body: { date: val('dp-date') } });
    document.getElementById('dp-result').innerHTML = r.created
      ? `<div class="msg ok">已生成 ${r.created} 张工单：${r.assigned} 张已按排班指派消杀队，${r.unassigned} 张待人工指派。${r.orders.filter(o => o.chemical_warning).length ? '⚠️ 部分工单触发「药剂不足」异常，请在工单中协同处理。' : ''}</div>`
      : `<div class="msg ok">${esc(r.message)}</div>`;
    setTimeout(() => switchTab('dispatch'), 1500);
  });
}

/* ---------- 工单列表 ---------- */
async function renderOrders(el) {
  const isStreetLike = ['street', 'supervisor'].includes(me.role);
  el.innerHTML = `
  <div class="card"><h3>工单列表</h3>
    <div class="filter-row">
      <select id="od-status"><option value="">全部状态</option>${opts(meta.order_status_labels)}</select>
      ${isStreetLike ? `<select id="od-comm"><option value="">全部小区</option>${commOpts()}</select>` : ''}
      <button class="btn sm" onclick="loadOrders()">查询</button>
    </div>
    <div id="orders-table"></div>
  </div>`;
  await loadOrders();
}
async function loadOrders() {
  const st = document.getElementById('od-status').value;
  const comm = document.getElementById('od-comm') ? document.getElementById('od-comm').value : '';
  let q = '/api/work-orders?';
  if (st) q += 'status=' + st + '&';
  if (comm) q += 'community_id=' + comm;
  const list = await api(q);
  document.getElementById('orders-table').innerHTML = `<div class="table-wrap"><table>
    <tr><th>工单号</th><th>小区</th><th>来源</th><th>消杀队</th><th>计划日期</th><th>优先级</th><th>复查期限</th><th>状态</th><th>操作</th></tr>
    ${list.map(o => `<tr>
      <td>${esc(o.order_no)}</td><td>${esc(o.community_name)}</td><td>${esc(o.source_desc)}</td>
      <td>${esc(o.team_name || '待指派')}</td><td>${esc(o.scheduled_date || '-')}</td>
      <td>${o.priority.toFixed(1)}</td>
      <td>${o.recheck_due_at ? fmtT(o.recheck_due_at) + (o.recheck_overdue ? ' ' + badge('已逾期', 'b-red') : '') : '-'}</td>
      <td>${sBadge(o.status, o.status_label)}</td>
      <td><button class="btn sm" onclick="openOrder(${o.id})">详情</button></td>
    </tr>`).join('') || '<tr><td colspan="9">暂无工单</td></tr>'}
  </table></div>`;
}

/* ---------- 工单详情弹窗 ---------- */
function closeModal() { document.getElementById('modal-mask').classList.add('hidden'); }
async function openOrder(id) {
  document.getElementById('modal-mask').classList.remove('hidden');
  document.getElementById('modal-body').innerHTML = '加载中...';
  await run(() => renderOrderDetail(id));
}
async function renderOrderDetail(id) {
  const d = await api('/api/work-orders/' + id);
  const o = d.order;
  const cc = d.close_conditions;
  const role = me.role;
  const condItem = (ok, name) => `<div>${ok ? '✅' : '⬜'} ${name}</div>`;

  let actions = '';
  // 消杀人员操作
  if (role === 'operator' && o.status !== 'closed') {
    const chems = chemCache || (chemCache = await api('/api/chemicals'));
    actions += `<div class="section"><h4>消杀作业</h4>
      <div class="btn-row" style="margin-top:0">
        ${['assigned', 'pending', 'escalated'].includes(o.status) ? `<button class="btn sm" onclick="orderAction(${id},'start')">到场开工</button>` : ''}
      </div>
      <div class="form-grid" style="margin-top:10px">
        <div class="form-item"><label>药剂 *</label><select id="tm-chem">${chems.map(c => `<option value="${c.id}">${esc(c.name)}（库存 ${c.stock}${esc(c.unit)}）</option>`).join('')}</select></div>
        <div class="form-item"><label>浓度</label><input id="tm-conc" placeholder="如 1:100 稀释"></div>
        <div class="form-item"><label>用量 *</label><input id="tm-used" type="number" step="0.1" placeholder="如 2.5"></div>
        <div class="form-item"><label>喷洒区域 *</label><input id="tm-area" placeholder="如 3号楼周边绿化带"></div>
        <div class="form-item full"><div class="check-row">
          <label><input type="checkbox" id="tm-sign"> 已设置警示牌</label>
          <label><input type="checkbox" id="tm-notify"> 已告知居民</label>
          <label><input type="checkbox" id="tm-pet"> 已宠物避让</label>
          <label><input type="checkbox" id="tm-newwp" onchange="document.getElementById('tm-newwp-desc').style.display=this.checked?'block':'none'"> 发现新积水点</label>
        </div></div>
        <div class="form-item full" id="tm-newwp-desc" style="display:none"><label>新积水点位置描述</label><input id="tm-newwp-text" placeholder="如 5号楼后废弃花盆积水"></div>
      </div>
      <div class="btn-row"><button class="btn" onclick="submitTreatment(${id})">提交消杀记录</button></div>
    </div>`;
  }
  // 复查（消杀/网格员/卫生监督/街道）
  if (['operator', 'grid', 'supervisor', 'street'].includes(role) && o.status !== 'closed') {
    actions += `<div class="section"><h4>复查登记</h4>
      <div class="inline-form">
        <select id="rc-result"><option value="pass">复查通过（无幼虫）</option><option value="larvae_found">仍有幼虫</option></select>
        <input id="rc-notes" placeholder="复查备注" style="width:280px">
        <button class="btn sm" onclick="submitRecheck(${id})">提交复查</button>
      </div>
    </div>`;
  }
  // 地下室排水沟长期积水 → 转物业整改（消杀队/网格员/街道）
  if (['operator', 'grid', 'street'].includes(role) && o.status !== 'closed') {
    const propUsers = (meta.users || []).filter(u => u.role === 'property');
    actions += `<div class="section"><h4>地下室长期积水转物业整改（消杀队只能临时处理时）</h4>
      <div class="inline-form">
        ${role === 'street' ? `<select id="pr2-facility"><option value="">本小区默认物业</option>${propUsers.map(u => `<option value="${u.id}">${esc(u.name)}</option>`).join('')}</select>` : ''}
        <input type="date" id="pr2-date" title="复查日期">
        <input id="pr2-repair" placeholder="排水维修要求" style="width:240px">
        <button class="btn sm orange" onclick="createOrderPRect(${id})">转物业整改</button>
      </div>
      <div style="font-size:12px;color:#90a4ae;margin-top:6px">仅关联地下室/地下车库积水点、且消杀队已提交过临时处理记录的工单可转；同一积水点反复消杀时系统也会自动生成。</div>
    </div>`;
  }
  // 异常上报（所有角色）
  if (o.status !== 'closed') {
    actions += `<div class="section"><h4>异常上报（居民拒绝入户 / 儿童活动区时段 / 宠物投诉 / 药剂不足 / 物业设施 / 仍有幼虫）</h4>
      <div class="inline-form">
        <select id="is-type">${opts(meta.issue_types)}</select>
        <input id="is-desc" placeholder="异常描述" style="width:320px">
        <button class="btn orange sm" onclick="submitIssue(${id})">上报异常</button>
      </div>
      <div style="font-size:12px;color:#90a4ae;margin-top:6px">上报后居民、物业、消杀队、街道、卫生监督将拉通到本工单协同处理。</div>
    </div>`;
  }
  // 街道操作
  if (role === 'street' && o.status !== 'closed') {
    const propUsers = (meta.users || []).filter(u => u.role === 'property');
    actions += `<div class="section"><h4>街道调度</h4>
      <div class="btn-row" style="margin-top:0">
        <span class="inline-form">指派：<select id="as-team">${teamOpts()}</select><input type="date" id="as-date"><button class="btn sm" onclick="assignOrder(${id})">指派消杀队</button></span>
      </div>
      <div class="form-grid" style="margin-top:12px">
        <div class="form-item"><label>整改责任物业</label><select id="rt-prop">${propUsers.map(u => `<option value="${u.id}">${esc(u.name)}</option>`).join('')}</select></div>
        <div class="form-item"><label>整改期限</label><input type="date" id="rt-deadline"></div>
        <div class="form-item full"><label>整改要求</label><input id="rt-desc" placeholder="如：清理楼顶水箱周边积水，检修排水设施"></div>
      </div>
      <div class="btn-row"><button class="btn sm orange" onclick="createRect(${id})">下发物业整改</button></div>
      <div class="form-grid" style="margin-top:12px">
        <div class="form-item"><label>居民告知标题</label><input id="nt-title" placeholder="如：消杀作业通知"></div>
        <div class="form-item full"><label>告知内容</label><input id="nt-content" placeholder="如：定于本周五上午对小区绿化带进行消杀，请居民看管好宠物"></div>
      </div>
      <div class="btn-row">
        <button class="btn sm" onclick="publishNotice(${id})">发布居民告知</button>
        <button class="btn sm" onclick="closeOrder(${id})">确认闭环</button>
      </div>
    </div>`;
  }

  document.getElementById('modal-body').innerHTML = `
    <span class="close-x" onclick="closeModal()">✕</span>
    <h2>工单 ${esc(o.order_no)} ${sBadge(o.status, o.status_label)}</h2>
    <div class="sub">创建于 ${fmtT(o.created_at)} ｜ 优先级 ${o.priority.toFixed(1)}${o.recheck_due_at ? ' ｜ 复查期限 ' + fmtT(o.recheck_due_at) : ''}</div>
    <div class="kv">
      <div><span class="k">小区：</span>${esc(o.community_name)}</div>
      <div><span class="k">消杀队：</span>${esc(o.team_name || '待指派')}</div>
      <div><span class="k">计划日期：</span>${esc(o.scheduled_date || '-')}</div>
      <div><span class="k">来源：</span>${esc(o.source_desc)}</div>
    </div>
    ${d.report ? `<div class="section"><h4>关联上报 ${esc(d.report.report_no)}</h4><div class="kv">
      <div><span class="k">类型：</span>${esc(d.report.type_label)}</div>
      <div><span class="k">位置：</span>${esc(d.report.location_desc)}</div>
      <div><span class="k">周边人群：</span>${esc(d.report.nearby_population || '-')}</div>
      <div><span class="k">宠物：</span>${d.report.has_pets ? '有' : '无'}</div>
      <div><span class="k">上报人：</span>${esc(d.report.reporter_name)}（${esc(meta.role_labels[d.report.reporter_role] || d.report.reporter_role)}）</div>
      <div><span class="k">照片：</span>${(d.report.photos || []).map(p => `<a href="${esc(p)}" target="_blank">查看</a>`).join(' ') || '-'}</div>
      <div style="grid-column:1/-1"><span class="k">描述：</span>${esc(d.report.description || '-')}</div>
    </div></div>` : ''}
    ${d.water_point ? `<div class="section"><h4>关联积水点 #${d.water_point.id}</h4><div class="kv">
      <div><span class="k">类型：</span>${esc(d.water_point.type_label)}</div>
      <div><span class="k">位置：</span>${esc(d.water_point.location_desc)}</div>
      <div><span class="k">状态：</span>${sBadge(d.water_point.status, d.water_point.status_label)}</div>
      <div><span class="k">幼虫：</span>${d.water_point.larvae_found ? badge('有幼虫', 'b-red') : '无'}</div>
      <div><span class="k">重点：</span>${d.water_point.is_key ? badge('重点 ' + d.water_point.key_reason, 'b-red') : '否'}</div>
    </div></div>` : ''}
    <div class="section"><h4>参与方（居民 / 物业 / 消杀队 / 街道 / 卫生监督）</h4>
      <div>${d.parties.map(p => badge((p.role_label) + (p.user_name ? '·' + p.user_name : ''), 'b-blue')).join(' ') || '暂无'}</div>
    </div>
    <div class="section"><h4>闭环条件</h4>
      <div style="display:flex;gap:18px;flex-wrap:wrap;font-size:13px">
        ${condItem(cc.treatment_done, '完成消杀')}${condItem(cc.recheck_passed, '复查通过')}
        ${condItem(cc.no_open_issues, '异常全部解决')}${condItem(cc.rectifications_verified, '整改核验')}
        ${condItem(cc.resident_notified, '居民告知')}
      </div>
    </div>
    ${d.treatments.length ? `<div class="section"><h4>消杀记录</h4><div class="table-wrap"><table>
      <tr><th>时间</th><th>操作员</th><th>药剂</th><th>浓度</th><th>用量</th><th>喷洒区域</th><th>警示牌</th><th>告知居民</th><th>宠物避让</th><th>新积水点</th></tr>
      ${d.treatments.map(t => `<tr><td>${fmtT(t.treated_at)}</td><td>${esc(t.operator_name)}</td><td>${esc(t.chemical_name)}</td>
        <td>${esc(t.concentration || '-')}</td><td>${t.chemical_used}</td><td>${esc(t.spray_area)}</td>
        <td>${t.warning_sign ? '✔' : '✘'}</td><td>${t.resident_notified ? '✔' : '✘'}</td><td>${t.pet_avoided ? '✔' : '✘'}</td>
        <td>${t.new_water_point_found ? esc(t.new_water_point_desc) : '-'}</td></tr>`).join('')}
    </table></div></div>` : ''}
    ${d.rechecks.length ? `<div class="section"><h4>复查记录</h4><div class="table-wrap"><table>
      <tr><th>时间</th><th>复查人</th><th>结果</th><th>备注</th></tr>
      ${d.rechecks.map(r => `<tr><td>${fmtT(r.created_at)}</td><td>${esc(r.inspector_name)}</td>
        <td>${r.result === 'pass' ? badge('通过', 'b-green') : badge('仍有幼虫', 'b-red')}</td><td>${esc(r.notes || '-')}</td></tr>`).join('')}
    </table></div></div>` : ''}
    ${d.issues.length ? `<div class="section"><h4>异常事项</h4><div class="table-wrap"><table>
      <tr><th>类型</th><th>描述</th><th>状态</th><th>处理结果</th><th>操作</th></tr>
      ${d.issues.map(i => `<tr><td>${esc(i.type_label)}</td><td>${esc(i.description)}</td>
        <td>${sBadge(i.status, i.status_label)}</td><td>${esc(i.resolution || '-')}</td>
        <td>${i.status === 'open' && ['street', 'supervisor'].includes(role)
          ? `<span class="inline-form"><input id="resolve-${i.id}" placeholder="处理结果"><button class="btn sm" onclick="resolveIssue(${id},${i.id})">解决</button></span>` : ''}
        </td></tr>`).join('')}
    </table></div></div>` : ''}
    ${d.rectifications.length ? `<div class="section"><h4>物业整改</h4><div class="table-wrap"><table>
      <tr><th>责任物业</th><th>要求</th><th>期限</th><th>状态</th><th>核验</th><th>操作</th></tr>
      ${d.rectifications.map(r => `<tr><td>${esc(r.property_name)}</td><td>${esc(r.description)}</td>
        <td>${esc(r.deadline || '-')}</td><td>${sBadge(r.status, r.status_label)}</td><td>${esc(r.verify_result || '-')}</td>
        <td>${r.status === 'done' && ['street', 'supervisor'].includes(role)
          ? `<span class="inline-form"><input id="verify-${r.id}" placeholder="核验意见"><button class="btn sm" onclick="verifyRect(${id},${r.id},true)">通过</button><button class="btn sm red" onclick="verifyRect(${id},${r.id},false)">不通过</button></span>` : ''}
        </td></tr>`).join('')}
    </table></div></div>` : ''}
    ${(d.property_rectifications || []).length ? `<div class="section"><h4>物业积水整改（地下室排水沟长期积水）</h4><div class="table-wrap"><table>
      <tr><th>单号</th><th>积水点位置</th><th>设施责任人</th><th>复查日期</th><th>处理方式</th><th>投诉变化</th><th>状态</th><th>操作</th></tr>
      ${d.property_rectifications.map(p => `<tr>
        <td>${esc(p.rect_no)}</td><td>${esc(p.water_type_label)}<br><b>${esc(p.water_location)}</b></td>
        <td>${esc(p.facility_name)}</td>
        <td>${esc(p.recheck_date_str)}${p.rect_overdue ? '<br>' + badge('整改超期', 'b-red') : ''}${p.recheck_overdue ? '<br>' + badge('复查超期', 'b-red') : ''}</td>
        <td>${esc(p.repair_method_label || '-')}</td>
        <td>${p.complaints_before} → ${p.complaints_after}</td>
        <td>${pBadge(p.status, p.status_label)}</td>
        <td>${['property', 'grid', 'operator', 'street', 'supervisor'].includes(role)
          ? `<button class="btn sm" onclick="closeModal();switchTab('prects');setTimeout(()=>openPRect(${p.id}),200)">详情</button>` : '-'}</td>
      </tr>`).join('')}
    </table></div></div>` : ''}
    ${actions}
    <div class="section"><h4>处理时间线</h4><div class="timeline">
      ${d.logs.map(l => `<div class="t-item"><b>${esc(l.action)}</b> ${esc(l.content || '')}
        <div class="t-meta">${fmtT(l.created_at)} ｜ ${esc(l.actor_name || '系统')}${l.role_label ? '（' + esc(l.role_label) + '）' : ''}</div></div>`).join('')}
    </div></div>`;
}
async function orderAction(id, action) { await run(async () => { const r = await api(`/api/work-orders/${id}/${action}`, { method: 'POST', body: {} }); toast(r.message); renderOrderDetail(id); }); }
async function submitTreatment(id) {
  await run(async () => {
    const body = {
      chemical_id: parseInt(val('tm-chem')), concentration: val('tm-conc'), spray_area: val('tm-area'),
      chemical_used: num('tm-used'), warning_sign: chk('tm-sign'), resident_notified: chk('tm-notify'),
      pet_avoided: chk('tm-pet'), new_water_point_found: chk('tm-newwp'),
      new_water_point_desc: chk('tm-newwp') ? val('tm-newwp-text') : '',
    };
    const r = await api(`/api/work-orders/${id}/treatments`, { method: 'POST', body });
    toast(r.message); chemCache = null; renderOrderDetail(id);
  });
}
async function submitRecheck(id) {
  await run(async () => {
    const r = await api(`/api/work-orders/${id}/rechecks`, { method: 'POST', body: { result: val('rc-result'), notes: val('rc-notes') } });
    toast(r.message); renderOrderDetail(id);
  });
}
async function submitIssue(id) {
  await run(async () => {
    const r = await api(`/api/work-orders/${id}/issues`, { method: 'POST', body: { type: val('is-type'), description: val('is-desc') } });
    toast(r.message); renderOrderDetail(id);
  });
}
async function resolveIssue(orderId, issueId) {
  await run(async () => {
    const r = await api(`/api/work-orders/${orderId}/issues/${issueId}/resolve`, { method: 'POST', body: { resolution: val('resolve-' + issueId) } });
    toast(r.message); renderOrderDetail(orderId);
  });
}
async function assignOrder(id) {
  await run(async () => {
    const r = await api(`/api/work-orders/${id}/assign`, { method: 'POST', body: { team_id: parseInt(val('as-team')), scheduled_date: val('as-date') } });
    toast(r.message); renderOrderDetail(id);
  });
}
async function createRect(id) {
  await run(async () => {
    const r = await api(`/api/work-orders/${id}/rectifications`, { method: 'POST', body: { property_user_id: parseInt(val('rt-prop')), description: val('rt-desc'), deadline: val('rt-deadline') } });
    toast(r.message); renderOrderDetail(id);
  });
}
async function createOrderPRect(id) {
  await run(async () => {
    const body = { repair_desc: val('pr2-repair') };
    if (val('pr2-date')) body.recheck_date = val('pr2-date');
    if (document.getElementById('pr2-facility') && val('pr2-facility')) body.facility_user_id = parseInt(val('pr2-facility'));
    const r = await api(`/api/work-orders/${id}/property-rectifications`, { method: 'POST', body });
    toast(r.message); renderOrderDetail(id);
  });
}
async function verifyRect(orderId, rectId, pass) {
  await run(async () => {
    const r = await api(`/api/rectifications/${rectId}/verify`, { method: 'POST', body: { pass, notes: val('verify-' + rectId) } });
    toast(r.message); renderOrderDetail(orderId);
  });
}
async function publishNotice(orderId) {
  await run(async () => {
    const r = await api('/api/notifications', { method: 'POST', body: { work_order_id: orderId, title: val('nt-title'), content: val('nt-content'), channel: 'app' } });
    toast(r.message); renderOrderDetail(orderId);
  });
}
async function closeOrder(id) {
  await run(async () => {
    const r = await api(`/api/work-orders/${id}/close`, { method: 'POST', body: {} });
    toast(r.message); renderOrderDetail(id);
  });
}

/* ---------- 整改任务（物业） ---------- */
async function renderRects(el) {
  const list = await api('/api/rectifications');
  el.innerHTML = `
  <div class="card"><h3>物业整改任务</h3><div class="table-wrap"><table>
    <tr><th>工单号</th><th>小区</th><th>整改要求</th><th>期限</th><th>状态</th><th>核验意见</th><th>操作</th></tr>
    ${list.map(r => `<tr>
      <td>${esc(r.order_no)}</td><td>${esc(r.community_name)}</td><td>${esc(r.description)}</td>
      <td>${esc(r.deadline || '-')}</td><td>${sBadge(r.status, r.status_label)}</td><td>${esc(r.verify_result || '-')}</td>
      <td>${r.status === 'pending' ? `<button class="btn sm" onclick="rectAction(${r.id},'start')">受理</button>` : ''}
          ${['pending', 'in_progress'].includes(r.status) ? `<button class="btn sm orange" onclick="rectAction(${r.id},'complete')">整改完成</button>` : ''}
          <button class="btn sm gray" onclick="openOrder(${r.work_order_id})">工单</button></td>
    </tr>`).join('') || '<tr><td colspan="7">暂无整改任务</td></tr>'}
  </table></div></div>`;
}
async function rectAction(id, action) { await run(async () => { const r = await api(`/api/rectifications/${id}/${action}`, { method: 'POST', body: {} }); toast(r.message); switchTab('rects'); }); }

/* ---------- 物业积水整改（地下室排水沟长期积水） ---------- */
const PRECT_COLOR = { pending: 'b-orange', rectifying: 'b-blue', recheck_pending: 'b-purple', verified: 'b-green', rejected: 'b-red' };
function pBadge(s, label) { return badge(label || s, PRECT_COLOR[s] || 'b-gray'); }

async function renderPRects(el) {
  const role = me.role;
  const canCreate = ['street', 'grid'].includes(role);
  el.innerHTML = `
  ${canCreate ? `<div class="card"><h3>地下室积水建档并下发物业整改</h3>
    <div class="form-grid">
      <div class="form-item"><label>小区 ${role === 'grid' ? '' : '*'}</label><select id="pr-comm" ${role === 'grid' ? 'disabled' : ''}>${commOpts(me.community_id)}</select></div>
      <div class="form-item"><label>积水点位置 *</label><input id="pr-loc" placeholder="如：地下车库 B2 层排水沟 / 集水井"></div>
      <div class="form-item"><label>设施责任人（物业）</label><select id="pr-facility"><option value="">本小区默认物业</option>${
        (meta.users || []).filter(u => u.role === 'property').map(u => `<option value="${u.id}">${esc(u.name)}${u.community_id ? '' : ''}</option>`).join('')}</select></div>
      <div class="form-item"><label>复查日期</label><input type="date" id="pr-date"></div>
      <div class="form-item full"><label>排水维修要求</label><input id="pr-repair" placeholder="如：疏通排水沟、检修排水泵，根治长期积水"></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="createPRect()">下发整改任务</button></div>
    <div style="font-size:12px;color:#90a4ae;margin-top:8px">消杀队对地下室排水沟长期积水重复临时处理时，系统也会自动生成整改任务，带上设施责任人与复查日期。</div>
  </div>` : ''}
  <div class="card"><h3>物业积水整改（临时处理 → 排水维修 → 按图复查；超期影响物业考核，街道可督办）</h3>
    <div class="filter-row">
      <select id="prf-status"><option value="">全部状态</option>${opts(meta.prop_rect_status_labels)}</select>
      <label><input type="checkbox" id="prf-overdue"> 仅看超期</label>
      <button class="btn sm" onclick="loadPRects()">查询</button>
    </div>
    <div id="prects-table"></div>
  </div>`;
  await loadPRects();
}
async function loadPRects() {
  let q = '/api/property-rectifications?';
  const stEl = document.getElementById('prf-status');
  if (stEl && stEl.value) q += 'status=' + stEl.value + '&';
  if (document.getElementById('prf-overdue') && document.getElementById('prf-overdue').checked) q += 'overdue=1';
  const list = await api(q);
  document.getElementById('prects-table').innerHTML = `<div class="table-wrap"><table>
    <tr><th>单号</th><th>小区</th><th>积水点位置</th><th>临时处理</th><th>设施责任人</th><th>复查日期</th><th>居民投诉变化</th><th>状态</th><th>操作</th></tr>
    ${list.map(p => `<tr style="${(p.rect_overdue || p.recheck_overdue) ? 'background:#fff5f5' : ''}">
      <td>${esc(p.rect_no)}</td><td>${esc(p.community_name)}</td>
      <td>${esc(p.water_type_label)}<br><b>${esc(p.water_location)}</b></td>
      <td>${p.temp_treatment_times} 次${p.temp_treated_by_name ? '<br><span style="font-size:11px;color:#90a4ae">' + esc(p.temp_treated_by_name) + '</span>' : ''}</td>
      <td>${esc(p.facility_name)}${p.facility_phone ? '<br><span style="font-size:11px;color:#90a4ae">' + esc(p.facility_phone) + '</span>' : ''}</td>
      <td>${esc(p.recheck_date_str)}
        ${p.rect_overdue ? '<br>' + badge('整改超期', 'b-red') : ''}${p.recheck_overdue ? '<br>' + badge('复查超期', 'b-red') : ''}
        ${p.supervise_count ? '<br>' + badge('督办×' + p.supervise_count, 'b-orange') : ''}</td>
      <td>${p.status === 'verified' || p.status === 'rejected' || p.complaints_after > 0
        ? `${p.complaints_before} → ${p.complaints_after} ` + (p.complaint_decreased === null || p.complaint_decreased === undefined ? '' : (p.complaint_decreased ? badge('下降', 'b-green') : badge('未下降', 'b-red')))
        : `基线 ${p.complaints_before} 起`}</td>
      <td>${pBadge(p.status, p.status_label)}</td>
      <td><button class="btn sm" onclick="openPRect(${p.id})">详情</button></td>
    </tr>`).join('') || '<tr><td colspan="9">暂无积水整改任务</td></tr>'}
  </table></div>`;
}
async function createPRect() {
  await run(async () => {
    const body = {
      community_id: parseInt(val('pr-comm')) || (me.community_id || 0),
      water_location: val('pr-loc'),
      repair_desc: val('pr-repair'),
    };
    if (val('pr-facility')) body.facility_user_id = parseInt(val('pr-facility'));
    if (val('pr-date')) body.recheck_date = val('pr-date');
    const r = await api('/api/property-rectifications', { method: 'POST', body });
    toast(r.message); switchTab('prects');
  });
}
async function openPRect(id) {
  document.getElementById('modal-mask').classList.remove('hidden');
  document.getElementById('modal-body').innerHTML = '加载中...';
  await run(() => renderPRectDetail(id));
}
async function renderPRectDetail(id) {
  const p = await api('/api/property-rectifications/' + id);
  const role = me.role;
  const photos = (arr) => (arr || []).map(u => `<a href="${esc(u)}" target="_blank" style="display:inline-block;margin:2px 6px 2px 0">📷 ${esc(u.split('/').pop())}</a>`).join('') || '暂无';

  let actions = '';
  if (role === 'property' && p.facility_user_id === me.id) {
    if (['pending', 'rejected'].includes(p.status)) {
      actions += `<div class="btn-row"><button class="btn" onclick="pRectAction(${id},'start')">受理整改（进场维修排水设施）</button></div>`;
    }
    if (['pending', 'rectifying', 'rejected'].includes(p.status)) {
      actions += `<div class="section"><h4>完成排水整改（整改照片须标明积水点位置与处理方式，供复查人员按图核验）</h4>
        <div class="form-grid">
          <div class="form-item"><label>处理方式 *</label><select id="pc-method">${opts(meta.prop_rect_method_labels)}</select></div>
          <div class="form-item full"><label>排水维修情况 *</label><input id="pc-repair" placeholder="如：清掏排水沟淤积、更换排水泵，沟通后无积水"></div>
          <div class="form-item full"><label>整改照片链接 *（多个用英文逗号分隔）</label><input id="pc-photos" placeholder="https://.../before.jpg, https://.../after.jpg"></div>
          <div class="form-item full"><label>照片标注说明 *（积水点位置 + 处理方式）</label><textarea id="pc-remark" placeholder="如：照片①位置=B2排水沟起点（红圈标注积水点），处理方式=疏通；照片②位置=集水井，处理方式=更换排水泵后复拍"></textarea></div>
        </div>
        <div class="btn-row"><button class="btn orange" onclick="submitPRectComplete(${id})">提交整改完成，转复查</button></div>
      </div>`;
    }
  }
  if (['operator', 'grid', 'street', 'supervisor'].includes(role) && p.status === 'recheck_pending') {
    actions += `<div class="section"><h4>复查核验（对照整改照片按图核验同一积水点）</h4>
      <div class="form-grid">
        <div class="form-item full"><label>复查照片链接 *（多个用英文逗号分隔）</label><input id="pv-photos" placeholder="https://.../recheck.jpg"></div>
        <div class="form-item full"><label>复查意见</label><textarea id="pv-remark" placeholder="对照整改照片同一位置/角度核验：排水沟是否已无积水、无孑孓"></textarea></div>
      </div>
      <div class="btn-row">
        <button class="btn" onclick="submitPRectRecheck(${id},true)">按图核验通过（积水清除）</button>
        <button class="btn red" onclick="submitPRectRecheck(${id},false)">不通过（退回返工）</button>
      </div>
    </div>`;
  }
  if (role === 'street' && p.status !== 'verified') {
    actions += `<div class="section"><h4>街道督办（整改/复查超期时提示督办，并通知物业负责人）</h4>
      <div class="inline-form">
        <input id="ps-content" placeholder="督办意见，如：请于 2 日内完成 B2 排水沟维修，否则纳入物业考核扣分" style="width:420px">
        <button class="btn sm orange" onclick="submitPRectSupervise(${id})">发出督办</button>
      </div>
    </div>`;
  }
  if (p.work_order_id) {
    actions += `<div class="btn-row"><button class="btn sm gray" onclick="openOrder(${p.work_order_id})">查看关联工单</button></div>`;
  }

  document.getElementById('modal-body').innerHTML = `
    <span class="close-x" onclick="closeModal()">✕</span>
    <h2>积水整改 ${esc(p.rect_no)} ${pBadge(p.status, p.status_label)}</h2>
    <div class="sub">${esc(p.community_name)} ｜ 创建 ${fmtT(p.created_at)} ｜ 复查日期 <b>${esc(p.recheck_date_str)}</b>
      ${p.rect_overdue ? badge('整改超期', 'b-red') : ''}${p.recheck_overdue ? badge('复查超期', 'b-red') : ''}</div>
    <div class="kv">
      <div><span class="k">积水点：</span>${esc(p.water_type_label)} · <b>${esc(p.water_location)}</b></div>
      <div><span class="k">设施责任人：</span>${esc(p.facility_name)} ${esc(p.facility_phone)}</div>
      ${p.order_no ? `<div><span class="k">关联工单：</span>${esc(p.order_no)}</div>` : ''}
    </div>
    <div class="section"><h4>消杀队临时处理（仅临时抑制，未根治设施积水）</h4><div class="kv">
      <div><span class="k">临时处理：</span>${esc(p.temp_treatment || '-')}</div>
      <div><span class="k">处理次数：</span>${p.temp_treatment_times} 次（${esc(p.temp_treated_by_name || '-')}，${fmtT(p.temp_treated_at)}）</div>
    </div></div>
    <div class="section"><h4>物业排水维修 / 整改照片（标明位置与处理方式）</h4>
      ${p.repair_desc ? `<div class="kv">
        <div><span class="k">处理方式：</span>${esc(p.repair_method_label)}</div>
        <div style="grid-column:1/-1"><span class="k">维修情况：</span>${esc(p.repair_desc)}</div>
        <div style="grid-column:1/-1"><span class="k">整改照片：</span>${photos(p.rectify_photos)}</div>
        <div style="grid-column:1/-1"><span class="k">照片标注：</span>${esc(p.rectify_photo_remark || '-')}</div>
        <div><span class="k">整改人：</span>${esc(p.rectified_by_name || '-')} ${fmtT(p.rectified_at)}</div>
      </div>` : '<div style="color:#90a4ae">物业尚未提交整改</div>'}
    </div>
    <div class="section"><h4>复查（复查人员按图核验）</h4>
      ${p.rechecked_at ? `<div class="kv">
        <div><span class="k">结果：</span>${p.recheck_result === 'pass' ? badge('按图核验通过', 'b-green') : badge('不通过/返工', 'b-red')}</div>
        <div><span class="k">复查人：</span>${esc(p.rechecked_by_name || '-')} ${fmtT(p.rechecked_at)}</div>
        <div style="grid-column:1/-1"><span class="k">复查照片：</span>${photos(p.recheck_photos)}</div>
        <div style="grid-column:1/-1"><span class="k">复查意见：</span>${esc(p.recheck_remark || '-')}</div>
      </div>` : '<div style="color:#90a4ae">待复查人员对照整改照片按图核验</div>'}
    </div>
    <div class="section"><h4>居民投诉变化</h4>
      <div>整改前 60 天同位置投诉 <b>${p.complaints_before}</b> 起 → 复查时 <b>${p.complaints_after}</b> 起
      ${p.complaint_decreased === null || p.complaint_decreased === undefined ? '' : (p.complaint_decreased ? badge('投诉下降', 'b-green') : badge('投诉未下降', 'b-red'))}</div>
    </div>
    <div class="section"><h4>超期提示与街道督办</h4>
      ${(p.reminders || []).length ? p.reminders.map(m => `<div class="banner ${m.kind === 'supervise' ? '' : 'danger'}" style="margin-bottom:8px">
        ${badge(m.kind_label, m.kind === 'supervise' ? 'b-orange' : 'b-red')} ${esc(m.content)}
        <div class="t-meta">${fmtT(m.created_at)}${m.raised_by_name ? ' ｜ ' + esc(m.raised_by_name) : ''}</div></div>`).join('') : '<div style="color:#90a4ae">暂无超期/督办记录</div>'}
    </div>
    ${actions}`;
}
async function pRectAction(id, action) {
  await run(async () => { const r = await api(`/api/property-rectifications/${id}/${action}`, { method: 'POST', body: {} }); toast(r.message); renderPRectDetail(id); });
}
async function submitPRectComplete(id) {
  await run(async () => {
    const photos = val('pc-photos') ? val('pc-photos').split(',').map(s => s.trim()).filter(Boolean) : [];
    const r = await api(`/api/property-rectifications/${id}/complete`, { method: 'POST', body: {
      repair_desc: val('pc-repair'), repair_method: val('pc-method'), rectify_photos: photos, rectify_photo_remark: val('pc-remark'),
    } });
    toast(r.message); renderPRectDetail(id);
  });
}
async function submitPRectRecheck(id, pass) {
  await run(async () => {
    const photos = val('pv-photos') ? val('pv-photos').split(',').map(s => s.trim()).filter(Boolean) : [];
    const r = await api(`/api/property-rectifications/${id}/recheck`, { method: 'POST', body: { pass, recheck_photos: photos, recheck_remark: val('pv-remark') } });
    toast(r.message); renderPRectDetail(id);
  });
}
async function submitPRectSupervise(id) {
  await run(async () => {
    const r = await api(`/api/property-rectifications/${id}/supervise`, { method: 'POST', body: { content: val('ps-content') } });
    toast(r.message); renderPRectDetail(id);
  });
}

/* ---------- 居民告知 ---------- */
async function renderNotices(el) {
  const list = await api('/api/notifications');
  const canPub = ['street', 'property'].includes(me.role);
  el.innerHTML = `
  ${canPub ? `<div class="card"><h3>发布居民告知</h3>
    <div class="form-grid">
      <div class="form-item"><label>小区</label><select id="nc-comm" ${me.community_id ? 'disabled' : ''}>${commOpts(me.community_id)}</select></div>
      <div class="form-item"><label>渠道</label><select id="nc-channel"><option value="app">站内通知</option><option value="board">公告栏</option><option value="sms">短信</option></select></div>
      <div class="form-item"><label>标题</label><input id="nc-title" placeholder="如：消杀作业通知"></div>
      <div class="form-item full"><label>内容</label><textarea id="nc-content" placeholder="告知消杀时间、区域、注意事项（宠物避让、儿童活动区时段等）"></textarea></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="pubNotice()">发布</button></div>
  </div>` : ''}
  <div class="card"><h3>居民告知</h3>
    ${list.map(n => `<div class="banner" style="margin-bottom:10px">
      <b>${esc(n.title || '居民告知')}</b>（${esc(n.community_name)} · ${esc(n.channel)} · ${fmtT(n.created_at)}${n.sent_by ? ' · ' + esc(n.sent_by) : ''}）<br>${esc(n.content)}
    </div>`).join('') || '暂无告知'}
  </div>`;
}
async function pubNotice() {
  await run(async () => {
    const r = await api('/api/notifications', { method: 'POST', body: { community_id: parseInt(val('nc-comm')), title: val('nc-title'), content: val('nc-content'), channel: val('nc-channel') } });
    toast(r.message); switchTab('notices');
  });
}

/* ---------- 药剂库存 ---------- */
async function renderChems(el) {
  const list = await api('/api/chemicals');
  const isStreet = me.role === 'street';
  el.innerHTML = `
  ${isStreet ? `<div class="card"><h3>新增药剂</h3>
    <div class="form-grid">
      <div class="form-item"><label>名称</label><input id="ch-name"></div>
      <div class="form-item"><label>单位</label><input id="ch-unit" value="升"></div>
      <div class="form-item"><label>初始库存</label><input id="ch-stock" type="number" step="0.1"></div>
      <div class="form-item"><label>安全库存</label><input id="ch-safe" type="number" step="0.1"></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="addChem()">添加</button></div>
  </div>` : ''}
  <div class="card"><h3>药剂库存（低于安全库存将自动触发「药剂不足」异常）</h3><div class="table-wrap"><table>
    <tr><th>药剂</th><th>库存</th><th>安全库存</th><th>状态</th><th>更新时间</th>${isStreet ? '<th>补货</th>' : ''}</tr>
    ${list.map(c => `<tr>
      <td>${esc(c.name)}</td><td>${c.stock} ${esc(c.unit)}</td><td>${c.safe_stock} ${esc(c.unit)}</td>
      <td>${c.low ? badge('库存不足', 'b-red') : badge('正常', 'b-green')}</td><td>${fmtT(c.updated_at)}</td>
      ${isStreet ? `<td><span class="inline-form"><input id="restock-${c.id}" type="number" step="0.1" style="width:80px"><button class="btn sm" onclick="restock(${c.id})">补货</button></span></td>` : ''}
    </tr>`).join('')}
  </table></div></div>`;
}
async function addChem() {
  await run(async () => {
    await api('/api/chemicals', { method: 'POST', body: { name: val('ch-name'), unit: val('ch-unit'), stock: num('ch-stock'), safe_stock: num('ch-safe') } });
    toast('已添加'); chemCache = null; switchTab('chems');
  });
}
async function restock(id) {
  await run(async () => {
    const r = await api(`/api/chemicals/${id}/restock`, { method: 'POST', body: { amount: num('restock-' + id) } });
    toast(r.message); chemCache = null; switchTab('chems');
  });
}

/* ---------- 排班 ---------- */
async function renderSched(el) {
  const [teams, list] = await Promise.all([api('/api/teams'), api('/api/schedules')]);
  el.innerHTML = `
  <div class="card"><h3>消杀队排班</h3>
    <div class="form-grid">
      <div class="form-item"><label>消杀队</label><select id="sc-team">${teams.map(t => `<option value="${t.id}">${esc(t.name)}（${t.members}人）</option>`).join('')}</select></div>
      <div class="form-item"><label>日期</label><input type="date" id="sc-date"></div>
      <div class="form-item"><label>班次</label><select id="sc-shift"><option value="allday">全天</option><option value="am">上午</option><option value="pm">下午</option></select></div>
      <div class="form-item"><label>可接工单数</label><input id="sc-max" type="number" value="5"></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="addSched()">保存排班</button></div>
  </div>
  <div class="card"><h3>排班与接单情况</h3><div class="table-wrap"><table>
    <tr><th>日期</th><th>消杀队</th><th>班次</th><th>可接单</th><th>已接单</th><th>剩余</th></tr>
    ${list.map(s => `<tr><td>${esc(s.work_date)}</td><td>${esc(s.team_name)}</td><td>${esc(s.shift)}</td>
      <td>${s.max_orders}</td><td>${s.used}</td><td>${s.max_orders - s.used}</td></tr>`).join('') || '<tr><td colspan="6">暂无排班</td></tr>'}
  </table></div></div>`;
}
async function addSched() {
  await run(async () => {
    await api(`/api/teams/${val('sc-team')}/schedules`, { method: 'POST', body: { work_date: val('sc-date'), shift: val('sc-shift'), max_orders: parseInt(val('sc-max')) || 5 } });
    toast('排班已保存'); switchTab('sched');
  });
}

/* ---------- 降雨 ---------- */
async function renderRainfall(el) {
  const list = await api('/api/rainfall');
  const canAdd = ['street', 'grid'].includes(me.role);
  el.innerHTML = `
  ${canAdd ? `<div class="card"><h3>录入降雨</h3>
    <div class="form-grid">
      <div class="form-item"><label>小区</label><select id="rf-comm">${commOpts(me.community_id)}</select></div>
      <div class="form-item"><label>日期</label><input type="date" id="rf-date"></div>
      <div class="form-item"><label>降雨量（毫米）</label><input id="rf-mm" type="number" step="0.1"></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="addRain()">保存</button></div>
  </div>` : ''}
  <div class="card"><h3>近 14 天降雨记录（派单评分因子）</h3><div class="table-wrap"><table>
    <tr><th>日期</th><th>小区</th><th>降雨量</th></tr>
    ${list.map(r => `<tr><td>${esc(r.rain_date)}</td><td>${esc(r.community_name)}</td><td>${r.amount_mm} mm</td></tr>`).join('') || '<tr><td colspan="3">暂无记录</td></tr>'}
  </table></div></div>`;
}
async function addRain() {
  await run(async () => {
    await api('/api/rainfall', { method: 'POST', body: { community_id: parseInt(val('rf-comm')), rain_date: val('rf-date'), amount_mm: num('rf-mm') } });
    toast('已保存'); switchTab('rainfall');
  });
}

/* ---------- 重点风险期 ---------- */
async function renderRisk(el) {
  const data = await api('/api/risk-periods');
  const cur = data.current;
  const today = new Date().toISOString().slice(0, 10);
  el.innerHTML = `
  ${cur ? `<div class="banner danger">⚠️ 当前生效：「${esc(cur.name)}」${esc(cur.disease)}（${cur.start_date} ~ ${cur.end_date}），复查间隔 <b>${cur.recheck_interval_days} 天</b>，派单优先级 ×1.5。</div>`
        : `<div class="banner ok">当前无生效中的重点风险期。</div>`}
  <div class="card"><h3>新增重点风险期（如登革热高发期）</h3>
    <div class="form-grid">
      <div class="form-item"><label>名称</label><input id="rk-name" placeholder="如：登革热重点防控期"></div>
      <div class="form-item"><label>疾病</label><input id="rk-disease" value="登革热"></div>
      <div class="form-item"><label>开始日期</label><input type="date" id="rk-start" value="${today}"></div>
      <div class="form-item"><label>结束日期</label><input type="date" id="rk-end"></div>
      <div class="form-item"><label>复查间隔（天）</label><input id="rk-interval" type="number" value="3"></div>
      <div class="form-item"><label>&nbsp;</label><div class="check-row"><label><input type="checkbox" id="rk-active" checked> 立即生效</label></div></div>
    </div>
    <div class="btn-row"><button class="btn" onclick="addRisk()">保存</button></div>
  </div>
  <div class="card"><h3>风险期列表</h3><div class="table-wrap"><table>
    <tr><th>名称</th><th>疾病</th><th>起止</th><th>复查间隔</th><th>状态</th><th>操作</th></tr>
    ${data.list.map(r => `<tr><td>${esc(r.name)}</td><td>${esc(r.disease)}</td><td>${r.start_date} ~ ${r.end_date}</td>
      <td>${r.recheck_interval_days} 天</td><td>${r.active ? badge('生效中', 'b-red') : badge('未生效', 'b-gray')}</td>
      <td>${r.active ? `<button class="btn sm gray" onclick="riskToggle(${r.id},false)">结束</button>` : `<button class="btn sm orange" onclick="riskToggle(${r.id},true)">开启</button>`}</td>
    </tr>`).join('') || '<tr><td colspan="6">暂无</td></tr>'}
  </table></div></div>`;
}
async function addRisk() {
  await run(async () => {
    const r = await api('/api/risk-periods', { method: 'POST', body: { name: val('rk-name'), disease: val('rk-disease'), start_date: val('rk-start'), end_date: val('rk-end'), recheck_interval_days: parseInt(val('rk-interval')) || 3, active: chk('rk-active') } });
    toast(r.message); switchTab('risk');
  });
}
async function riskToggle(id, activate) {
  await run(async () => {
    const r = await api(`/api/risk-periods/${id}/${activate ? 'activate' : 'deactivate'}`, { method: 'POST', body: {} });
    toast(r.message); switchTab('risk');
  });
}

/* ---------- 闭环看板 ---------- */
async function renderLoop(el) {
  const data = await api('/api/dashboard/closed-loop');
  const trend = await api('/api/dashboard/complaint-trend');
  const rp = data.risk_period;
  const months = [...new Set(trend.map(t => t.month))].sort();
  const comms = [...new Set(trend.map(t => t.community))];
  el.innerHTML = `
  ${rp ? `<div class="banner danger">⚠️「${esc(rp.name)}」生效中：复查间隔 ${data.recheck_interval_days} 天，重点积水点加密复查。</div>` : ''}
  <div class="card"><h3>各小区「投诉 → 派单 → 消杀 → 复查 → 整改 → 闭环」进度</h3><div class="table-wrap"><table>
    <tr><th>小区</th><th>投诉</th><th>待派单</th><th>工单</th><th>已消杀</th><th>已复查</th><th>复查通过</th><th>复查逾期</th><th>整改中</th><th>整改已核验</th><th>积水整改</th><th>整改超期</th><th>积水已复查</th><th>已闭环</th><th>闭环率</th><th>重点积水点</th><th>本月投诉</th><th>上月投诉</th><th>投诉下降</th></tr>
    ${data.rows.map(r => `<tr>
      <td><b>${esc(r.community_name)}</b></td><td>${r.reports_total}</td><td>${r.reports_pending}</td>
      <td>${r.orders_total}</td><td>${r.orders_treated}</td><td>${r.orders_rechecked}</td>
      <td>${r.recheck_pass}</td><td>${r.recheck_overdue ? badge(r.recheck_overdue + ' 单', 'b-red') : 0}</td>
      <td>${r.rectifications_open}</td><td>${r.rectifications_verified}</td>
      <td>${r.prop_rect_open || 0}</td>
      <td>${r.prop_rect_overdue ? badge(r.prop_rect_overdue + ' 单', 'b-red') : 0}</td>
      <td>${r.prop_rect_verified || 0}</td>
      <td>${r.orders_closed}</td><td><b>${r.close_rate.toFixed(0)}%</b></td>
      <td>${r.key_water_points ? badge(r.key_water_points + ' 处', 'b-red') : 0}</td>
      <td>${r.complaints_this_month}</td><td>${r.complaints_last_month}</td>
      <td>${r.complaints_last_month ? (r.complaint_decline_pct >= 0 ? badge('↓' + r.complaint_decline_pct.toFixed(0) + '%', 'b-green') : badge('↑' + (-r.complaint_decline_pct).toFixed(0) + '%', 'b-red')) : '-'}</td>
    </tr>`).join('')}
  </table></div></div>
  <div class="card"><h3>近 6 个月投诉趋势</h3><div class="table-wrap"><table>
    <tr><th>月份</th>${comms.map(c => `<th>${esc(c)}</th>`).join('')}</tr>
    ${months.map(m => `<tr><td>${m}</td>${comms.map(c => { const p = trend.find(t => t.month === m && t.community === c); return `<td>${p ? p.count : 0}</td>`; }).join('')}</tr>`).join('')}
  </table></div></div>`;
}

/* ---------- 考核看板 ---------- */
async function renderAssess(el) {
  const month = new Date().toISOString().slice(0, 7);
  el.innerHTML = `
  <div class="card"><h3>街道考核（复查结果 / 药剂消耗 / 投诉下降 / 整改责任）</h3>
    <div class="filter-row"><label>考核月份 <input type="month" id="as-month" value="${month}"></label>
    <button class="btn sm" onclick="loadAssess()">查询</button></div>
    <div id="assess-body"></div>
  </div>`;
  await loadAssess();
}
async function loadAssess() {
  const data = await api('/api/dashboard/assessment?month=' + document.getElementById('as-month').value);
  document.getElementById('assess-body').innerHTML = `
  <h4 style="margin:8px 0;color:#33691e">小区维度</h4><div class="table-wrap"><table>
    <tr><th>小区</th><th>本月投诉</th><th>上月投诉</th><th>投诉下降</th><th>闭环工单</th><th>平均闭环时长</th><th>复查通过率</th><th>药剂消耗</th><th>物业设施问题</th><th>整改核验</th><th>积水整改(新)</th><th>整改超期</th></tr>
    ${data.communities.map(r => `<tr>
      <td><b>${esc(r.community_name)}</b></td><td>${r.complaints}</td><td>${r.complaints_prev}</td>
      <td>${r.complaints_prev ? (r.complaint_decline_pct >= 0 ? badge('↓' + r.complaint_decline_pct.toFixed(0) + '%', 'b-green') : badge('↑' + (-r.complaint_decline_pct).toFixed(0) + '%', 'b-red')) : '-'}</td>
      <td>${r.orders_closed}</td><td>${r.avg_close_hours ? r.avg_close_hours.toFixed(1) + ' 小时' : '-'}</td>
      <td>${r.recheck_total ? r.recheck_pass_rate.toFixed(0) + '%（' + r.recheck_pass + '/' + r.recheck_total + '）' : '-'}</td>
      <td>${r.chemical_used.toFixed(1)}</td>
      <td>${r.property_facility_issues ? badge(r.property_facility_issues + ' 起', 'b-orange') : 0}</td>
      <td>${r.rectifications_verified}/${r.rectifications_total}</td>
      <td>${r.prop_rect_verified || 0}/${r.prop_rect_total || 0}${r.prop_rect_recheck_fail ? ' ' + badge('返工' + r.prop_rect_recheck_fail, 'b-red') : ''}</td>
      <td>${r.prop_rect_overdue ? badge(r.prop_rect_overdue + ' 起', 'b-red') : 0}</td>
    </tr>`).join('')}
  </table></div>
  <h4 style="margin:16px 0 8px;color:#33691e">物业设施责任人维度（地下室积水整改超期影响物业考核）</h4><div class="table-wrap"><table>
    <tr><th>设施责任人</th><th>小区</th><th>积水整改任务</th><th>已复查通过</th><th>复查合格率</th><th>整改/复查超期</th><th>街道督办</th><th>投诉变化(前→后)</th></tr>
    ${(data.facilities || []).map(f => `<tr>
      <td><b>${esc(f.user_name)}</b></td><td>${esc(f.community_name)}</td>
      <td>${f.rect_total}</td><td>${f.rect_verified}</td>
      <td>${(f.rect_verified + f.recheck_fail) ? f.recheck_pass_rate.toFixed(0) + '%' : '-'}</td>
      <td>${f.rect_overdue ? badge(f.rect_overdue + ' 起超期', 'b-red') : badge('无超期', 'b-green')}</td>
      <td>${f.supervise_count ? badge(f.supervise_count + ' 次', 'b-orange') : 0}</td>
      <td>${f.complaints_before} → ${f.complaints_after} ${f.complaint_delta < 0 ? badge('↓投诉下降', 'b-green') : (f.complaint_delta > 0 ? badge('↑投诉上升', 'b-red') : '持平')}</td>
    </tr>`).join('') || '<tr><td colspan="8">本月暂无积水整改任务</td></tr>'}
  </table></div>
  <h4 style="margin:16px 0 8px;color:#33691e">消杀队维度</h4><div class="table-wrap"><table>
    <tr><th>消杀队</th><th>接单数</th><th>消杀次数</th><th>药剂消耗</th><th>复查通过率</th></tr>
    ${data.teams.map(t => `<tr><td><b>${esc(t.team_name)}</b></td><td>${t.orders_assigned}</td><td>${t.treatments}</td>
      <td>${t.chemical_used.toFixed(1)}</td><td>${t.recheck_total ? t.recheck_pass_rate.toFixed(0) + '%（' + t.recheck_pass + '/' + t.recheck_total + '）' : '-'}</td></tr>`).join('')}
  </table></div>`;
}

/* ---------- 儿童活动区错峰消杀 ---------- */
const PLAN_COLOR = { planned: 'b-gray', notified: 'b-blue', treated: 'b-purple', warning_removed: 'b-orange', confirmed: 'b-green' };
function planBadge(p) { return badge(p.status_label, PLAN_COLOR[p.status]); }

async function renderZones(el) {
  const role = me.role;
  const zones = await api('/api/child-zones');
  const plans = await api('/api/child-zone-plans');
  let head = '';

  if (role === 'street') {
    const kgUsers = (meta.users || []).filter(u => u.role === 'kindergarten');
    const orders = (await api('/api/work-orders')).filter(o => o.status !== 'closed');
    head = `
    <div class="card"><h3>儿童活动区管理</h3>
      <div class="form-grid">
        <div class="form-item"><label>小区</label><select id="cz-comm">${commOpts()}</select></div>
        <div class="form-item"><label>活动区名称</label><input id="cz-name" placeholder="如：阳光幼儿园旁绿化带"></div>
        <div class="form-item"><label>类型</label><select id="cz-type">${opts(meta.child_zone_types)}</select></div>
        <div class="form-item"><label>园方联系人</label><input id="cz-contact" placeholder="姓名"></div>
        <div class="form-item"><label>联系电话</label><input id="cz-phone" placeholder="手机"></div>
        <div class="form-item"><label>园方账号（可确认）</label><select id="cz-user"><option value="">不绑定</option>${kgUsers.map(u => `<option value="${u.id}">${esc(u.name)}</option>`).join('')}</select></div>
        <div class="form-item"><label>儿童活动时段（错峰依据）</label><input id="cz-activity" placeholder="07:30-08:30,16:00-18:00"></div>
        <div class="form-item"><label>家长群</label><input id="cz-group" placeholder="如：阳光幼儿园家长一群"></div>
      </div>
      <div class="btn-row"><button class="btn" onclick="addZone()">保存活动区</button></div>
    </div>
    <div class="card"><h3>新建错峰消杀计划（按儿童活动时间 / 风向 / 药剂安全间隔 / 园方联系人安排）</h3>
      <div class="form-grid">
        <div class="form-item"><label>儿童活动区 *</label><select id="cp-zone">${zones.map(z => `<option value="${z.id}">${esc(z.name)}（活动时段 ${esc(z.activity_times || '无')}）</option>`).join('')}</select></div>
        <div class="form-item"><label>关联工单（选填）</label><select id="cp-order"><option value="">不关联</option>${orders.map(o => `<option value="${o.id}">${esc(o.order_no)} ${esc(o.source_desc)}</option>`).join('')}</select></div>
        <div class="form-item"><label>计划开始 *</label><input type="datetime-local" id="cp-start"></div>
        <div class="form-item"><label>计划结束 *</label><input type="datetime-local" id="cp-end"></div>
        <div class="form-item"><label>风向</label><select id="cp-wind">${(meta.wind_directions || []).map(w => `<option>${w}</option>`).join('')}</select></div>
        <div class="form-item"><label>药剂安全间隔（小时）</label><input type="number" id="cp-interval" value="4" step="0.5"></div>
      </div>
      <div class="btn-row"><button class="btn" onclick="addPlan()">生成计划并推送提醒</button></div>
      <div style="font-size:12px;color:#90a4ae;margin-top:8px">作业时段 + 安全间隔若与儿童活动时间重叠将被拒绝；创建后自动向幼儿园、家长群、附近居民推送含避让时段与联系人的提醒。</div>
    </div>`;
  }

  const canOperate = ['street', 'operator'].includes(role);
  el.innerHTML = head + `
  <div class="card"><h3>${role === 'resident' ? '儿童活动区消杀安排与恢复时间' : '错峰消杀计划'}</h3><div class="table-wrap"><table>
    <tr><th>计划号</th><th>活动区</th><th>小区</th><th>作业时段</th><th>避让时段（含安全间隔）</th><th>风向</th><th>安全间隔</th><th>儿童活动恢复时间</th><th>园方联系人</th><th>关联工单</th><th>状态</th><th>操作</th></tr>
    ${plans.map(p => `<tr>
      <td>${esc(p.plan_no)}</td>
      <td>${esc(p.zone_name)}<br><span style="font-size:11px;color:#90a4ae">${esc(p.zone_type_label)} · 活动时段 ${esc(p.activity_times || '-')}</span></td>
      <td>${esc(p.community_name)}</td>
      <td>${fmtT(p.planned_start)}<br>~ ${fmtT(p.planned_end)}</td>
      <td>${fmtT(p.planned_start)} ~ ${p.recovery_time ? fmtT(p.recovery_time) : '-'}</td>
      <td>${esc(p.wind_direction || '-')}</td>
      <td>${p.safety_interval_hours} 小时</td>
      <td>${p.status === 'confirmed' && p.recovery_time ? badge(fmtT(p.recovery_time), 'b-green') : (p.recovery_time ? fmtT(p.recovery_time) + '<br>' + badge('待园方确认', 'b-orange') : '-')}</td>
      <td>${esc(p.contact_name)} ${esc(p.contact_phone)}</td>
      <td>${p.order_no ? esc(p.order_no) : '-'}</td>
      <td>${planBadge(p)}</td>
      <td>
        <button class="btn sm gray" onclick="openPlanReminders(${p.id})">提醒</button>
        ${canOperate && p.status === 'treated' ? `<button class="btn sm orange" onclick="removeWarning(${p.id})">撤除警示</button>` : ''}
        ${role === 'kindergarten' && p.status === 'warning_removed' ? `<span class="inline-form"><input id="confirm-${p.id}" placeholder="确认意见"><button class="btn sm" onclick="confirmPlan(${p.id})">园方确认</button></span>` : ''}
      </td>
    </tr>`).join('') || '<tr><td colspan="12">暂无计划</td></tr>'}
  </table></div>
  ${role === 'resident' ? '<div style="font-size:12px;color:#78909c;margin-top:10px">园方确认后，本页与「居民告知」会展示药剂安全间隔与儿童活动恢复时间。</div>' : ''}
  </div>`;
}
async function addZone() {
  await run(async () => {
    const body = {
      community_id: parseInt(val('cz-comm')), name: val('cz-name'), zone_type: val('cz-type'),
      contact_name: val('cz-contact'), contact_phone: val('cz-phone'),
      activity_times: val('cz-activity'), parent_group: val('cz-group'),
    };
    if (val('cz-user')) body.contact_user_id = parseInt(val('cz-user'));
    await api('/api/child-zones', { method: 'POST', body });
    toast('儿童活动区已保存'); switchTab('zones');
  });
}
async function addPlan() {
  await run(async () => {
    const body = {
      child_zone_id: parseInt(val('cp-zone')),
      planned_start: val('cp-start'), planned_end: val('cp-end'),
      wind_direction: val('cp-wind'), safety_interval_hours: num('cp-interval') || 4,
    };
    if (val('cp-order')) body.work_order_id = parseInt(val('cp-order'));
    await api('/api/child-zone-plans', { method: 'POST', body });
    toast('错峰计划已生成，提醒已推送幼儿园/家长群/附近居民'); switchTab('zones');
  });
}
async function openPlanReminders(planId) {
  document.getElementById('modal-mask').classList.remove('hidden');
  document.getElementById('modal-body').innerHTML = '加载中...';
  await run(async () => {
    const d = await api('/api/child-zone-plans/' + planId);
    const p = d.plan;
    const canDeliver = ['street', 'operator', 'kindergarten'].includes(me.role);
    document.getElementById('modal-body').innerHTML = `
      <span class="close-x" onclick="closeModal()">✕</span>
      <h2>计划 ${esc(p.plan_no)} ${planBadge(p)}</h2>
      <div class="sub">${esc(p.zone_name)} ｜ 作业 ${fmtT(p.planned_start)} ~ ${fmtT(p.planned_end)} ｜ 风向 ${esc(p.wind_direction || '-')} ｜ 安全间隔 ${p.safety_interval_hours}h ｜ 恢复 ${p.recovery_time ? fmtT(p.recovery_time) : '-'}</div>
      <div class="section"><h4>作业提醒（幼儿园 / 家长群 / 附近居民，含避让时段与联系人，保留送达状态）</h4>
      <div class="table-wrap"><table>
        <tr><th>对象</th><th>渠道</th><th>避让时段</th><th>联系人</th><th>内容</th><th>送达状态</th><th>操作</th></tr>
        ${d.reminders.map(r => `<tr>
          <td>${esc(r.audience_label)}</td><td>${esc(r.channel)}</td><td>${esc(r.avoid_period)}</td><td>${esc(r.contact_info)}</td>
          <td style="max-width:320px">${esc(r.content)}</td>
          <td>${badge(r.delivery_status_label, r.delivery_status === 'delivered' ? 'b-green' : (r.delivery_status === 'sent' ? 'b-blue' : 'b-gray'))}${r.delivered_at ? '<br><span style="font-size:11px;color:#90a4ae">' + fmtT(r.delivered_at) + '</span>' : ''}</td>
          <td>${canDeliver && r.delivery_status === 'sent' ? `<button class="btn sm" onclick="deliverReminder(${p.id},${r.id})">确认送达</button>` : ''}</td>
        </tr>`).join('')}
      </table></div></div>`;
  });
}
async function deliverReminder(planId, rid) {
  await run(async () => { const r = await api(`/api/child-zone-plans/${planId}/reminders/${rid}/deliver`, { method: 'POST', body: {} }); toast(r.message); openPlanReminders(planId); });
}
async function removeWarning(planId) {
  await run(async () => { const r = await api(`/api/child-zone-plans/${planId}/remove-warning`, { method: 'POST', body: {} }); toast(r.message); switchTab('zones'); });
}
async function confirmPlan(planId) {
  await run(async () => {
    const note = val('confirm-' + planId) || '现场确认无误';
    const r = await api(`/api/child-zone-plans/${planId}/confirm`, { method: 'POST', body: { note } });
    toast(r.message); switchTab('zones');
  });
}

/* ---------- 宠物误触投诉 ---------- */
async function renderPets(el) {
  const role = me.role;
  if (role === 'resident') {
    const list = await api('/api/pet-complaints');
    el.innerHTML = `
    <div class="card"><h3>宠物误触投诉（自动关联本小区最近一次消杀的药剂、喷洒区域与警示时间）</h3>
      <div class="form-grid">
        <div class="form-item"><label>宠物类型 *</label><select id="pc-type">${opts(meta.pet_types)}</select></div>
        <div class="form-item"><label>宠物名字</label><input id="pc-name" placeholder="如：豆豆"></div>
        <div class="form-item full"><label>不适症状 *</label><textarea id="pc-symptom" placeholder="如：喷药后呕吐、打喷嚏、精神萎靡"></textarea></div>
        <div class="form-item full"><label>主人行走路线 *</label><input id="pc-route" placeholder="如：18:00 从 3 号楼沿中心花园遛狗至东门"></div>
        <div class="form-item full"><label>宠物就医凭证链接（多个用英文逗号分隔）</label><input id="pc-vouchers" placeholder="https://example.com/vet1.jpg"></div>
      </div>
      <div class="btn-row"><button class="btn" onclick="submitPetComplaint()">提交投诉</button></div>
    </div>
    <div class="card"><h3>我的投诉</h3><div class="table-wrap"><table>
      <tr><th>单号</th><th>宠物</th><th>症状</th><th>关联药剂</th><th>喷洒区域</th><th>警示时间</th><th>状态</th><th>处理结果</th><th>操作</th></tr>
      ${list.map(p => `<tr>
        <td>${esc(p.complaint_no)}</td><td>${esc(p.pet_type_label)}${p.pet_name ? '·' + esc(p.pet_name) : ''}</td>
        <td>${esc(p.symptom)}</td><td>${esc(p.chemical_name || '-')}</td><td>${esc(p.spray_area || '-')}</td>
        <td>${fmtT(p.warning_time)}</td><td>${sBadge(p.status, p.status_label)}</td>
        <td>${p.status === 'resolved' ? `回访:${p.need_revisit ? '是' : '否'} 赔付:${p.compensation ? '是' : '否'}${p.notify_adjustment ? '<br>告知调整:' + esc(p.notify_adjustment) : ''}` : '-'}</td>
        <td><button class="btn sm gray" onclick="openPet(${p.id})">详情</button></td>
      </tr>`).join('') || '<tr><td colspan="9">暂无投诉</td></tr>'}
    </table></div></div>`;
    return;
  }
  // 街道 / 卫生监督
  const list = await api('/api/pet-complaints');
  const tracking = await api('/api/pet-complaints/tracking');
  el.innerHTML = `
  <div class="card"><h3>同类宠物投诉跟踪（告知方式调整后是否下降）</h3><div class="table-wrap"><table>
    <tr><th>小区</th><th>投诉总数</th><th>待处理</th><th>最近告知调整</th><th>调整前(30天)</th><th>调整后(至今)</th><th>日均对比</th><th>是否下降</th></tr>
    ${tracking.map(t => `<tr>
      <td><b>${esc(t.community_name)}</b></td><td>${t.total_complaints}</td><td>${t.pending_complaints}</td>
      <td>${t.latest_adjustment ? esc(t.latest_adjustment) + '<br><span style="font-size:11px;color:#90a4ae">' + fmtT(t.adjusted_at) + '</span>' : '-'}</td>
      <td>${t.adjusted_at ? t.before_count : '-'}</td><td>${t.adjusted_at ? t.after_count : '-'}</td>
      <td>${t.adjusted_at ? t.before_per_day.toFixed(2) + ' → ' + t.after_per_day.toFixed(2) : '-'}</td>
      <td>${t.decreased === null || t.decreased === undefined ? '-' : (t.decreased ? badge('已下降', 'b-green') : badge('未下降', 'b-red'))}</td>
    </tr>`).join('')}
  </table></div></div>
  <div class="card"><h3>宠物误触投诉处理</h3><div class="table-wrap"><table>
    <tr><th>单号</th><th>小区</th><th>投诉人</th><th>宠物</th><th>症状</th><th>行走路线</th><th>关联药剂/区域/警示时间</th><th>就医凭证</th><th>状态</th><th>操作</th></tr>
    ${list.map(p => `<tr>
      <td>${esc(p.complaint_no)}</td><td>${esc(p.community_name)}</td><td>${esc(p.reporter_name)}</td>
      <td>${esc(p.pet_type_label)}${p.pet_name ? '·' + esc(p.pet_name) : ''}</td><td>${esc(p.symptom)}</td>
      <td>${esc(p.walking_route)}</td>
      <td>${esc(p.chemical_name || '-')}<br>${esc(p.spray_area || '-')}<br><span style="font-size:11px;color:#90a4ae">${fmtT(p.warning_time)}</span></td>
      <td>${(p.medical_vouchers || []).map(v => `<a href="${esc(v)}" target="_blank">凭证</a>`).join(' ') || '-'}</td>
      <td>${sBadge(p.status, p.status_label)}</td>
      <td>${p.status === 'pending' ? `<button class="btn sm orange" onclick="openHandlePet(${p.id})">办结</button>` : `<button class="btn sm gray" onclick="openPet(${p.id})">详情</button>`}</td>
    </tr>`).join('') || '<tr><td colspan="10">暂无投诉</td></tr>'}
  </table></div></div>`;
}
async function submitPetComplaint() {
  await run(async () => {
    const vouchers = val('pc-vouchers') ? val('pc-vouchers').split(',').map(s => s.trim()).filter(Boolean) : [];
    await api('/api/pet-complaints', { method: 'POST', body: {
      pet_type: val('pc-type'), pet_name: val('pc-name'), symptom: val('pc-symptom'),
      walking_route: val('pc-route'), medical_vouchers: vouchers,
    } });
    toast('投诉已提交，已自动关联本小区最近一次消杀记录'); switchTab('pets');
  });
}
async function openPet(id) {
  document.getElementById('modal-mask').classList.remove('hidden');
  document.getElementById('modal-body').innerHTML = '加载中...';
  await run(async () => {
    const p = await api('/api/pet-complaints/' + id);
    document.getElementById('modal-body').innerHTML = `
      <span class="close-x" onclick="closeModal()">✕</span>
      <h2>宠物投诉 ${esc(p.complaint_no)} ${sBadge(p.status, p.status_label)}</h2>
      <div class="sub">${esc(p.community_name)} ｜ 投诉人 ${esc(p.reporter_name)} ｜ ${fmtT(p.created_at)}</div>
      <div class="kv">
        <div><span class="k">宠物：</span>${esc(p.pet_type_label)}${p.pet_name ? '·' + esc(p.pet_name) : ''}</div>
        <div><span class="k">症状：</span>${esc(p.symptom)}</div>
        <div><span class="k">行走路线：</span>${esc(p.walking_route)}</div>
        <div><span class="k">关联工单：</span>${esc(p.order_no || '-')}</div>
        <div><span class="k">药剂：</span>${esc(p.chemical_name || '-')}</div>
        <div><span class="k">喷洒区域：</span>${esc(p.spray_area || '-')}</div>
        <div><span class="k">警示时间：</span>${fmtT(p.warning_time)}</div>
        <div><span class="k">就医凭证：</span>${(p.medical_vouchers || []).map(v => `<a href="${esc(v)}" target="_blank">查看</a>`).join(' ') || '-'}</div>
      </div>
      ${p.status === 'resolved' ? `<div class="section"><h4>处理结果（${esc(p.handler_name || '')} ${fmtT(p.handled_at)}）</h4><div class="kv">
        <div><span class="k">需要回访：</span>${p.need_revisit ? '是' : '否'}</div>
        <div><span class="k">需要赔付：</span>${p.compensation ? '是' : '否'} ${esc(p.compensation_note || '')}</div>
        <div><span class="k">药剂说明：</span>${esc(p.chemical_note || '-')}</div>
        <div><span class="k">后续告知调整：</span>${esc(p.notify_adjustment || '-')}</div>
        <div><span class="k">处理备注：</span>${esc(p.resolution_note || '-')}</div>
      </div></div>` : ''}`;
  });
}
async function openHandlePet(id) {
  document.getElementById('modal-mask').classList.remove('hidden');
  await run(async () => {
    const p = await api('/api/pet-complaints/' + id);
    document.getElementById('modal-body').innerHTML = `
      <span class="close-x" onclick="closeModal()">✕</span>
      <h2>办结宠物投诉 ${esc(p.complaint_no)}</h2>
      <div class="sub">${esc(p.community_name)} ｜ ${esc(p.pet_type_label)}${p.pet_name ? '·' + esc(p.pet_name) : ''} ｜ 药剂 ${esc(p.chemical_name || '-')} ｜ 区域 ${esc(p.spray_area || '-')}</div>
      <div class="form-grid">
        <div class="form-item full"><div class="check-row">
          <label><input type="checkbox" id="hp-revisit"> 需要回访</label>
          <label><input type="checkbox" id="hp-comp"> 需要赔付</label>
        </div></div>
        <div class="form-item full"><label>赔付说明</label><input id="hp-compnote" placeholder="如：凭就医票据报销"></div>
        <div class="form-item full"><label>药剂说明（成分/安全间隔/注意事项）</label><textarea id="hp-chemnote" placeholder="如：高效氯氟氰菊酯 1:100 稀释，安全间隔 4 小时，对猫科动物敏感"></textarea></div>
        <div class="form-item full"><label>后续告知方式调整</label><textarea id="hp-adjust" placeholder="如：改为短信+公告栏双通道，提前 48 小时告知并标注宠物避让路线"></textarea></div>
        <div class="form-item full"><label>处理备注</label><input id="hp-note" placeholder="处理过程与结论"></div>
      </div>
      <div class="btn-row"><button class="btn" onclick="handlePet(${p.id})">提交办结</button></div>`;
  });
}
async function handlePet(id) {
  await run(async () => {
    const r = await api(`/api/pet-complaints/${id}/handle`, { method: 'POST', body: {
      need_revisit: chk('hp-revisit'), compensation: chk('hp-comp'), compensation_note: val('hp-compnote'),
      chemical_note: val('hp-chemnote'), notify_adjustment: val('hp-adjust'), resolution_note: val('hp-note'),
    } });
    toast(r.message); closeModal(); switchTab('pets');
  });
}

/* ---------- 小区消杀档案 ---------- */
async function renderArchive(el) {
  el.innerHTML = `
  <div class="card"><h3>小区消杀档案（消杀记录 / 药剂消耗 / 宠物投诉与告知调整跟踪）</h3>
    <div class="filter-row"><select id="ar-comm">${commOpts()}</select>
    <button class="btn sm" onclick="loadArchive()">查询</button></div>
    <div id="archive-body"></div>
  </div>`;
  await loadArchive();
}
async function loadArchive() {
  const id = document.getElementById('ar-comm').value;
  const a = await api('/api/communities/' + id + '/archive');
  const t = a.pet_tracking;
  document.getElementById('archive-body').innerHTML = `
    <div class="stat-grid">
      <div class="stat"><div class="num">${a.orders_total}</div><div class="lbl">工单总数</div></div>
      <div class="stat"><div class="num">${a.orders_closed}</div><div class="lbl">已闭环</div></div>
      <div class="stat"><div class="num">${a.treatments_total}</div><div class="lbl">消杀次数</div></div>
      <div class="stat"><div class="num">${a.pet_complaints.total}</div><div class="lbl">宠物投诉</div></div>
      <div class="stat"><div class="num">${a.pet_complaints.need_revisit}</div><div class="lbl">需回访</div></div>
      <div class="stat"><div class="num">${a.pet_complaints.compensation}</div><div class="lbl">需赔付</div></div>
    </div>
    <div class="section"><h4>药剂消耗</h4>
      ${a.chemical_usage.map(c => `<div>${esc(c.name)}：<b>${c.used.toFixed(1)}</b></div>`).join('') || '暂无'}
    </div>
    <div class="section"><h4>宠物投诉跟踪（进入下次计划参考）</h4>
      ${t.adjusted_at ? `<div>最近告知调整：<b>${esc(t.latest_adjustment)}</b>（${fmtT(t.adjusted_at)}）</div>
      <div>调整前 30 天投诉 ${t.before_count} 起（日均 ${t.before_per_day.toFixed(2)}） → 调整后 ${t.after_count} 起（日均 ${t.after_per_day.toFixed(2)}）
      ${t.decreased ? badge('同类投诉已下降', 'b-green') : badge('同类投诉未下降', 'b-red')}</div>` : '暂无告知调整记录'}
      ${a.notify_adjustments.length ? '<div style="margin-top:8px">' + a.notify_adjustments.map(x => `<div>· ${esc(x.complaint_no)}：${esc(x.notify_adjustment)}</div>`).join('') + '</div>' : ''}
    </div>
    <div class="section"><h4>最近居民告知</h4>
      ${a.recent_notifications.map(n => `<div>· <b>${esc(n.title)}</b> ${esc(n.content)} <span style="color:#90a4ae">${fmtT(n.created_at)}</span></div>`).join('') || '暂无'}
    </div>`;
}

/* ---------- 路由表 ---------- */
const TAB_RENDERERS = {
  report: renderReport,
  reports: renderReports,
  points: renderPoints,
  keylist: renderKeyList,
  dispatch: renderDispatch,
  orders: renderOrders,
  rects: renderRects,
  prects: renderPRects,
  notices: renderNotices,
  chems: renderChems,
  sched: renderSched,
  rainfall: renderRainfall,
  risk: renderRisk,
  loop: renderLoop,
  assess: renderAssess,
  zones: renderZones,
  pets: renderPets,
  archive: renderArchive,
};

/* ---------- 启动 ---------- */
(async function init() {
  if (token && me) {
    try { me = await api('/api/auth/me'); localStorage.setItem('me', JSON.stringify(me)); await boot(); return; }
    catch (e) { /* token 失效，回到登录页 */ }
  }
  renderLogin();
})();
