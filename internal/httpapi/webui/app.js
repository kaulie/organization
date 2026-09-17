// organization 组织架构管理 —— 前端脚本。
//
// 纯原生 JS，无第三方依赖（与后端「仅标准库」的约束一致）：所有数据都来自
// 同源 JSON API /api/v1/*。
(() => {
  'use strict';

  const API = '/api/v1';

  const state = {
    departments: [],
    departmentTypes: [],
    persons: [],
    personTypes: [],
    memberCounts: new Map(),
  };

  const $ = (id) => document.getElementById(id);

  /* ---------------- helpers ---------------- */

  // esc renders untrusted values as text: names/ids are user supplied.
  function esc(value) {
    return String(value ?? '').replace(/[&<>"']/g, (ch) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    })[ch]);
  }

  function formatTime(iso) {
    if (!iso) return '—';
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return esc(iso);
    const pad = (n) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
      ` ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  }

  let toastTimer = null;
  // Department currently shown in the detail drawer (null when closed): used to
  // refresh the drawer's title after a rename.
  let drawerDepartmentId = null;
  // Department targeted by the rename dialog (null when closed).
  let renameDepartmentId = null;
  function toast(message, kind = 'ok') {
    const node = $('toast');
    node.textContent = message;
    node.className = `toast toast-${kind}`;
    node.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { node.hidden = true; }, 4200);
  }

  // api calls the JSON API and turns error payloads into Error(message).
  async function api(path, options = {}) {
    const response = await fetch(API + path, options);
    const raw = await response.text();
    let payload = null;
    if (raw.trim() !== '') {
      try { payload = JSON.parse(raw); } catch { /* non-JSON error body */ }
    }
    if (!response.ok) {
      const detail = payload && payload.error ? payload.error.message : `HTTP ${response.status}`;
      throw new Error(detail);
    }
    return payload;
  }

  async function withPending(button, task) {
    const label = button.textContent;
    button.disabled = true;
    button.textContent = '处理中…';
    try {
      return await task();
    } finally {
      button.disabled = false;
      button.textContent = label;
    }
  }

  /* ---------------- rendering ---------------- */

  function fillSelect(select, values, { keepValue = true } = {}) {
    const previous = keepValue ? select.value : '';
    select.innerHTML = values
      .map((v) => `<option value="${esc(v.value)}">${esc(v.label)}</option>`)
      .join('');
    if (previous && values.some((v) => v.value === previous)) select.value = previous;
  }

  function renderStats() {
    const humans = state.persons.filter((p) => p.type === 'HUMAN').length;
    const agents = state.persons.length - humans;
    $('stat-departments').textContent = state.departments.length;
    $('stat-persons').textContent = state.persons.length;
    $('stat-humans').textContent = humans;
    $('stat-agents').textContent = agents;

    const byType = new Map();
    for (const dept of state.departments) {
      byType.set(dept.type, (byType.get(dept.type) || 0) + 1);
    }
    $('stat-department-types').textContent = byType.size
      ? [...byType].map(([type, n]) => `${type} ${n}`).join(' · ')
      : '暂无数据';
    $('stat-persons-hint').textContent = state.departments.length
      ? `分布在 ${new Set(state.persons.map((p) => p.departmentId)).size} 个部门`
      : '暂无数据';
  }

  function renderDepartments() {
    const body = $('department-rows');
    $('department-count').textContent = `${state.departments.length} 个`;
    $('department-empty').hidden = state.departments.length > 0;

    body.innerHTML = state.departments.map((dept) => {
      const members = state.memberCounts.get(dept.id);
      return `<tr>
        <td><code>${esc(dept.id)}</code></td>
        <td><button type="button" class="link" data-department="${esc(dept.id)}">${esc(dept.name)}</button></td>
        <td><span class="tag tag-type">${esc(dept.type)}</span></td>
        <td class="num">${members === undefined ? '—' : members}</td>
        <td>${formatTime(dept.createdAt)}</td>
        <td class="row-actions">
          <button type="button" class="ghost" data-rename="${esc(dept.id)}">重命名</button>
          <button type="button" class="ghost" data-department="${esc(dept.id)}">详情</button>
        </td>
      </tr>`;
    }).join('');
  }

  function visiblePersons() {
    const keyword = $('person-search').value.trim().toLowerCase();
    const departmentId = $('person-filter').value;
    return state.persons.filter((person) => {
      if (departmentId && person.departmentId !== departmentId) return false;
      if (!keyword) return true;
      return [person.name, person.id, person.employeeNo]
        .some((field) => String(field ?? '').toLowerCase().includes(keyword));
    });
  }

  function personRow(person, { withDepartment = true } = {}) {
    const tagClass = person.type === 'AGENT' ? 'tag-agent' : 'tag-human';
    const cells = [
      `<td><code>${esc(person.employeeNo)}</code></td>`,
      `<td>${esc(person.name)}</td>`,
      `<td><code>${esc(person.id)}</code></td>`,
      `<td><span class="tag ${tagClass}">${esc(person.type)}</span></td>`,
    ];
    if (withDepartment) cells.push(`<td>${esc(person.departmentName || person.departmentId)}</td>`);
    cells.push(`<td>${formatTime(person.createdAt)}</td>`);
    return `<tr>${cells.join('')}</tr>`;
  }

  function renderPersons() {
    const persons = visiblePersons();
    $('person-count').textContent = persons.length === state.persons.length
      ? `${state.persons.length} 人`
      : `${persons.length} / ${state.persons.length} 人`;
    $('person-empty').hidden = persons.length > 0;
    $('person-rows').innerHTML = persons.map((person) => personRow(person)).join('');
  }

  function closeDrawer() {
    drawerDepartmentId = null;
    $('department-drawer').hidden = true;
  }

  // openDepartment shows GET /api/v1/departments/{id} (department + members).
  async function openDepartment(id) {
    try {
      const detail = await api(`/departments/${encodeURIComponent(id)}`);
      drawerDepartmentId = detail.id;
      $('drawer-title').textContent = detail.name;
      $('drawer-meta').innerHTML =
        `<code>${esc(detail.id)}</code> · <span class="tag tag-type">${esc(detail.type)}</span>` +
        ` · 创建于 ${formatTime(detail.createdAt)} · ${detail.members.length} 名成员`;

      const members = detail.members || [];
      $('drawer-rows').innerHTML = members.map((person) => personRow(person, { withDepartment: false })).join('');
      $('drawer-empty').hidden = members.length > 0;
      $('department-drawer').hidden = false;
    } catch (err) {
      toast(`读取部门失败：${err.message}`, 'err');
    }
  }

  /* ---------------- rename dialog ---------------- */

  // openRename pops the rename dialog for a department, prefilled with its
  // current name. Renaming only touches the name: id, type and members stay.
  function openRename(id) {
    const dept = state.departments.find((d) => d.id === id);
    if (!dept) return;
    renameDepartmentId = dept.id;
    $('rename-target').innerHTML =
      `目标：<code>${esc(dept.id)}</code> · 当前名称「${esc(dept.name)}」`;
    const input = $('rename-name');
    input.value = dept.name;
    const dialog = $('rename-dialog');
    if (typeof dialog.showModal === 'function') {
      if (!dialog.open) dialog.showModal();
    } else {
      dialog.hidden = false; // very old browsers: fall back to a plain block
    }
    input.focus();
    input.select();
  }

  function closeRename() {
    renameDepartmentId = null;
    const dialog = $('rename-dialog');
    if (dialog.open) {
      dialog.close();
    } else {
      dialog.hidden = true;
    }
  }

  async function submitRename(event) {
    event.preventDefault();
    if (!renameDepartmentId) return;

    const button = event.target.querySelector('button[type="submit"]');
    const targetID = renameDepartmentId;
    const name = $('rename-name').value.trim();
    if (!name) {
      toast('部门名称不能为空', 'err');
      return;
    }

    try {
      const updated = await withPending(button, () => api(`/departments/${encodeURIComponent(targetID)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      }));
      closeRename();
      toast(`部门已重命名为 ${updated.name}`);
      // Reload both lists: the persons table renders each member's
      // departmentName, which the rename just changed.
      await loadDepartments();
      await loadPersons();
      if (drawerDepartmentId === targetID) await openDepartment(targetID);
    } catch (err) {
      toast(`重命名失败：${err.message}`, 'err');
    }
  }

  /* ---------------- data loading ---------------- */

  async function loadHealth() {
    const pill = $('health-pill');
    try {
      const response = await fetch('/health', { headers: { Accept: 'application/json' } });
      const payload = await response.json();
      pill.textContent = payload.status === 'ok' ? '服务正常' : `状态：${payload.status}`;
      pill.className = `pill ${payload.status === 'ok' ? 'pill-ok' : 'pill-warn'}`;
      $('service-version').textContent = payload.version || '—';
    } catch {
      pill.textContent = '无法连接服务';
      pill.className = 'pill pill-err';
    }
  }

  async function loadDepartments() {
    const payload = await api('/departments');
    state.departments = payload.items || [];
    if (payload.types) {
      state.departmentTypes = payload.types;
      fillSelect($('department-type'), payload.types.map((t) => ({ value: t, label: t })), { keepValue: false });
    }
    // Member counts come from the department detail endpoint; the list endpoint
    // only returns plain departments.
    const details = await Promise.all(
      state.departments.map((dept) => api(`/departments/${encodeURIComponent(dept.id)}`).catch(() => null)),
    );
    state.memberCounts = new Map();
    details.forEach((detail) => {
      if (detail) state.memberCounts.set(detail.id, (detail.members || []).length);
    });

    const departmentOptions = state.departments.map((dept) => ({
      value: dept.id,
      label: `${dept.name}（${dept.id}）`,
    }));
    fillSelect($('person-department'), departmentOptions);
    fillSelect($('person-filter'), [{ value: '', label: '全部部门' }, ...departmentOptions], { keepValue: false });

    renderDepartments();
    renderStats();
  }

  async function loadPersons() {
    const payload = await api('/persons');
    state.persons = payload.items || [];
    if (payload.types) {
      state.personTypes = payload.types;
      fillSelect($('person-type'), payload.types.map((t) => ({ value: t, label: t })), { keepValue: false });
    }
    renderPersons();
    renderStats();
  }

  async function refreshAll() {
    try {
      await loadDepartments();
      await loadPersons();
    } catch (err) {
      toast(`加载数据失败：${err.message}`, 'err');
    }
  }

  /* ---------------- forms ---------------- */

  async function submitDepartment(event) {
    event.preventDefault();
    const button = event.target.querySelector('button[type="submit"]');
    const payload = {
      name: $('department-name').value.trim(),
      type: $('department-type').value,
    };
    try {
      const created = await withPending(button, () => api('/departments', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }));
      toast(`已新增部门 ${created.name}（${created.id}）`);
      event.target.reset();
      await loadDepartments();
    } catch (err) {
      toast(`新增部门失败：${err.message}`, 'err');
    }
  }

  async function submitPerson(event) {
    event.preventDefault();
    const button = event.target.querySelector('button[type="submit"]');
    const payload = {
      name: $('person-name').value.trim(),
      id: $('person-id').value.trim(),
      type: $('person-type').value,
      departmentId: $('person-department').value,
    };
    try {
      const created = await withPending(button, () => api('/persons', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }));
      toast(`已注册 ${created.name}，工号 ${created.employeeNo}`);
      event.target.reset();
      await loadDepartments();
      await loadPersons();
    } catch (err) {
      toast(`注册人员失败：${err.message}`, 'err');
    }
  }

  /* ---------------- wiring ---------------- */

  function bind() {
    $('department-form').addEventListener('submit', submitDepartment);
    $('person-form').addEventListener('submit', submitPerson);

    $('refresh-departments').addEventListener('click', async (event) => {
      await withPending(event.currentTarget, loadDepartments);
      toast('部门列表已刷新');
    });
    $('refresh-persons').addEventListener('click', async (event) => {
      await withPending(event.currentTarget, loadPersons);
      toast('人员列表已刷新');
    });

    $('person-search').addEventListener('input', renderPersons);
    $('person-filter').addEventListener('change', renderPersons);

    // Department detail: the name link and the "详情" button carry the same hook.
    // The 重命名 button sits in the same row, so it is checked first.
    $('department-rows').addEventListener('click', (event) => {
      const rename = event.target.closest('[data-rename]');
      if (rename) {
        openRename(rename.dataset.rename);
        return;
      }
      const trigger = event.target.closest('[data-department]');
      if (trigger) openDepartment(trigger.dataset.department);
    });
    $('rename-form').addEventListener('submit', submitRename);
    $('rename-cancel').addEventListener('click', closeRename);
    $('drawer-rename').addEventListener('click', () => {
      if (drawerDepartmentId) openRename(drawerDepartmentId);
    });
    document.querySelectorAll('[data-close-drawer]').forEach((node) => {
      node.addEventListener('click', closeDrawer);
    });
    document.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape') return;
      // 先关闭最上层的重命名对话框，再关闭抽屉。
      if (renameDepartmentId) {
        closeRename();
        return;
      }
      closeDrawer();
    });
  }

  bind();
  loadHealth();
  refreshAll();
})();
