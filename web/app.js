const $ = (selector) => document.querySelector(selector)
const $$ = (selector) => [...document.querySelectorAll(selector)]
let currentUser = null
let platform = null

async function api(path, options = {}) {
  const res = await fetch(path, { credentials: 'same-origin', ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } })
  let payload
  try { payload = await res.json() } catch { throw new Error(`服务返回异常（HTTP ${res.status}）`) }
  if (!res.ok || !payload.success) throw new Error(payload.message || `请求失败（HTTP ${res.status}）`)
  return payload.data
}

function toast(message, error = false) {
  const node = $('#toast')
  node.textContent = message
  node.className = `toast show${error ? ' error' : ''}`
  clearTimeout(toast.timer)
  toast.timer = setTimeout(() => { node.className = 'toast' }, 3500)
}

function showLogin() {
  $('#login-view').classList.remove('hidden')
  $('#app-view').classList.add('hidden')
}

async function enterApp(user) {
  currentUser = user
  $('#login-view').classList.add('hidden')
  $('#app-view').classList.remove('hidden')
  $('#user-label').textContent = `${user.username} · ${user.role === 'admin' ? '管理员' : '用户'}`
  platform = await api('/api/platform')
  $('#target-label').textContent = platform.newapi_base_url || 'NEW API 未配置'
  $('#connection-badge').textContent = platform.newapi_configured ? '● New API 已连接' : '● New API 未配置'
  $('#connection-badge').classList.toggle('ok', platform.newapi_configured)
  if (user.role === 'admin') {
    $('#admin-view').classList.remove('hidden')
    $('#user-view').classList.add('hidden')
    await loadAdmin()
  } else {
    $('#admin-view').classList.add('hidden')
    $('#user-view').classList.remove('hidden')
    setupChannelTypes()
    await loadMetadata()
  }
}

$('#login-form').addEventListener('submit', async (event) => {
  event.preventDefault()
  const button = event.submitter
  button.disabled = true
  try {
    const data = Object.fromEntries(new FormData(event.currentTarget))
    await enterApp(await api('/api/auth/login', { method: 'POST', body: JSON.stringify(data) }))
  } catch (err) { toast(err.message, true) } finally { button.disabled = false }
})

$('#logout-button').addEventListener('click', async () => {
  try { await api('/api/auth/logout', { method: 'POST', body: '{}' }) } catch {}
  currentUser = null
  showLogin()
})

$('#show-create-user').addEventListener('click', () => $('#create-user-panel').classList.remove('hidden'))
$('#cancel-create-user').addEventListener('click', () => $('#create-user-panel').classList.add('hidden'))
$('#create-user-form').addEventListener('submit', async (event) => {
  event.preventDefault()
  const button = event.submitter
  button.disabled = true
  try {
    await api('/api/admin/users', { method: 'POST', body: JSON.stringify(Object.fromEntries(new FormData(event.currentTarget))) })
    event.currentTarget.reset(); $('#create-user-panel').classList.add('hidden'); toast('用户已创建'); await loadAdmin()
  } catch (err) { toast(err.message, true) } finally { button.disabled = false }
})

async function loadAdmin() {
  try {
    const [users, uploads, settings] = await Promise.all([api('/api/admin/users'), api('/api/admin/uploads'), api('/api/admin/settings')])
    $('#total-users').textContent = users.length
    $('#active-users').textContent = users.filter((user) => user.status === 1).length
    $('#upload-count').textContent = uploads.length
    $('#users-body').innerHTML = users.map((user) => `<tr><td><b>${escapeHTML(user.username)}</b><br><small>#${user.id}</small></td><td>${user.role === 'admin' ? '管理员' : '普通用户'}</td><td><span class="status ${user.status === 1 ? 'active' : ''}">${user.status === 1 ? '启用' : '停用'}</span></td><td>${formatTime(user.created_at)}</td><td>${user.role === 'admin' ? '—' : `<button class="ghost user-toggle" data-id="${user.id}" data-status="${user.status === 1 ? 2 : 1}">${user.status === 1 ? '停用' : '启用'}</button><button class="ghost user-password" data-id="${user.id}">重置密码</button>`}</td></tr>`).join('')
    $('#uploads-list').innerHTML = uploads.length ? uploads.map((item) => `<div class="audit-item ${item.success ? 'success' : 'failed'}"><b>${escapeHTML(item.channel_name)} · ${item.success ? '成功' : '失败'}</b><span>${escapeHTML(item.username)} / 类型 ${item.channel_type} / ${formatTime(item.created_at)}</span><span>${escapeHTML(item.message)}</span></div>`).join('') : '<div class="audit-item"><span>暂无记录</span></div>'
    renderAdminSettings(settings)
    $$('.user-toggle').forEach((button) => button.addEventListener('click', () => updateUser(button.dataset.id, { status: Number(button.dataset.status) })))
    $$('.user-password').forEach((button) => button.addEventListener('click', () => resetPassword(button.dataset.id)))
  } catch (err) { toast(err.message, true) }
}

function renderAdminSettings(settings) {
  const form = $('#newapi-settings-form')
  form.elements.newapi_base_url.value = settings.newapi_base_url || ''
  form.elements.newapi_user_id.value = settings.newapi_user_id || '1'
  form.elements.newapi_access_token.value = ''
  form.elements.newapi_access_token.placeholder = settings.has_access_token ? '已配置，留空保持不变' : '输入个人密钥'
  $('#token-hint').textContent = settings.has_access_token ? '已有加密密钥' : '未配置密钥'
  $('#settings-status').textContent = settings.configured ? '已连接' : '未配置'
  $('#settings-status').classList.toggle('active', settings.configured)
}

$('#newapi-settings-form').addEventListener('submit', async (event) => {
  event.preventDefault()
  const button = event.submitter
  button.disabled = true
  try {
    const body = Object.fromEntries(new FormData(event.currentTarget))
    const result = await api('/api/admin/settings', { method: 'PUT', body: JSON.stringify(body) })
    toast(`${result.message}，读取到 ${result.groups.length} 个分组`)
    platform = await api('/api/platform')
    $('#target-label').textContent = platform.newapi_base_url || 'NEW API 未配置'
    $('#connection-badge').textContent = platform.newapi_configured ? '● New API 已连接' : '● New API 未配置'
    $('#connection-badge').classList.toggle('ok', platform.newapi_configured)
    renderAdminSettings(await api('/api/admin/settings'))
  } catch (err) { toast(err.message, true) } finally { button.disabled = false }
})

async function updateUser(id, body) {
  try { await api(`/api/admin/users/${id}`, { method: 'PATCH', body: JSON.stringify(body) }); toast('用户已更新'); await loadAdmin() } catch (err) { toast(err.message, true) }
}

async function resetPassword(id) {
  const password = prompt('输入新密码（至少 8 位）')
  if (!password) return
  await updateUser(id, { password })
}

function setupChannelTypes() {
  $('#channel-type').innerHTML = platform.channel_types.map((item) => `<option value="${item.id}">${item.id} · ${escapeHTML(item.name)}</option>`).join('')
  updateBasePolicy()
}

$('#channel-type').addEventListener('change', updateBasePolicy)
function updateBasePolicy() {
  const anthropic = Number($('#channel-type').value) === 14
  $('#computed-base-url').textContent = anthropic ? platform.anthropic_base_url : '空'
  $('#base-policy').textContent = anthropic ? '固定 OpenRouter 地址' : '强制留空'
}

async function loadMetadata() {
  if (!platform.newapi_configured) { $('#groups').innerHTML = '<span class="muted">管理员尚未配置 New API 连接</span>'; return }
  try {
    const metadata = await api('/api/newapi/metadata')
    $('#groups').innerHTML = metadata.groups.length ? metadata.groups.map((group, index) => `<label><input type="checkbox" name="group" value="${escapeAttr(group)}" ${index === 0 ? 'checked' : ''}><span>${escapeHTML(group)}</span></label>`).join('') : '<span class="muted">New API 暂无分组</span>'
    $('#model-options').innerHTML = (metadata.models || []).map((model) => `<option value="${escapeAttr(model)}"></option>`).join('')
  } catch (err) { $('#groups').innerHTML = `<span class="muted">${escapeHTML(err.message)}</span>`; toast(err.message, true) }
}

const jsonFields = ['model_mapping', 'status_code_mapping', 'setting', 'param_override', 'header_override', 'settings']
function optionalJSON(form, field) {
  const raw = String(form.get(field) || '').trim()
  if (!raw) return undefined
  try { return JSON.stringify(JSON.parse(raw)) } catch { throw new Error(`${field} 不是有效 JSON`) }
}

$('#channel-form').addEventListener('submit', async (event) => {
  event.preventDefault()
  const button = event.submitter
  button.disabled = true
  try {
    const form = new FormData(event.currentTarget)
    const groups = form.getAll('group')
    if (!groups.length) throw new Error('至少选择一个分组')
    const channel = {
      name: form.get('name').trim(), type: Number(form.get('type')), key: form.get('key').trim(), models: form.get('models').trim(), group: groups.join(','),
      status: Number(form.get('status')), priority: Number(form.get('priority') || 0), weight: Number(form.get('weight') || 0), auto_ban: Number(form.get('auto_ban')),
    }
    for (const field of ['test_model', 'openai_organization', 'tag', 'remark', 'other']) {
      const value = String(form.get(field) || '').trim(); if (value) channel[field] = value
    }
    for (const field of jsonFields) { const value = optionalJSON(form, field); if (value !== undefined) channel[field] = value }
    const extraRaw = String(form.get('extra_json') || '').trim()
    if (extraRaw) {
      const extra = JSON.parse(extraRaw)
      if (!extra || Array.isArray(extra) || typeof extra !== 'object') throw new Error('额外渠道 JSON 必须是对象')
      Object.assign(channel, extra)
    }
    const body = { mode: form.get('mode'), multi_key_mode: form.get('multi_key_mode'), batch_add_set_key_prefix_2_name: form.get('batch_prefix') === 'on', channel }
    const result = await api('/api/channels', { method: 'POST', body: JSON.stringify(body) })
    toast(`${result.message}，Base URL：${result.base_url || '空'}`)
    event.currentTarget.reset(); updateBasePolicy(); await loadMetadata()
  } catch (err) { toast(err.message, true) } finally { button.disabled = false }
})

function escapeHTML(value) { const node = document.createElement('div'); node.textContent = String(value ?? ''); return node.innerHTML }
function escapeAttr(value) { return escapeHTML(value).replaceAll('"', '&quot;') }
function formatTime(value) { try { return new Date(value).toLocaleString('zh-CN', { hour12: false }) } catch { return value } }

;(async () => {
  try { await enterApp(await api('/api/me')) } catch { showLogin() }
})()
