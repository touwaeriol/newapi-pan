const $ = (selector) => document.querySelector(selector)
const $$ = (selector) => [...document.querySelectorAll(selector)]
let currentUser = null
let platform = null
let channelMetadata = { groups: [], models: [], all_models: [], prefill_groups: [] }

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
$('#generate-user-password').addEventListener('click', async () => {
  const password = randomPassword()
  $('#create-user-form').elements.password.value = password
  await copyText(password)
  toast('随机密码已生成并复制')
})
$('#create-user-form').addEventListener('submit', async (event) => {
  event.preventDefault()
  const button = event.submitter
  button.disabled = true
  try {
    const formData = Object.fromEntries(new FormData(event.currentTarget))
    await api('/api/admin/users', { method: 'POST', body: JSON.stringify(formData) })
    showCredential(formData.username, formData.password)
    event.currentTarget.reset(); $('#create-user-panel').classList.add('hidden'); toast('用户已创建，可复制凭据'); await loadAdmin()
  } catch (err) { toast(err.message, true) } finally { button.disabled = false }
})

async function loadAdmin() {
  try {
    const [users, uploads, settings] = await Promise.all([api('/api/admin/users'), api('/api/admin/uploads'), api('/api/admin/settings')])
    $('#total-users').textContent = users.length
    $('#active-users').textContent = users.filter((user) => user.status === 1).length
    $('#upload-count').textContent = uploads.length
    $('#users-body').innerHTML = users.map((user) => `<tr><td><b>${escapeHTML(user.username)}</b><br><small>#${user.id}</small></td><td>${user.role === 'admin' ? '管理员' : '普通用户'}</td><td><span class="status ${user.status === 1 ? 'active' : ''}">${user.status === 1 ? '启用' : '停用'}</span></td><td>${formatTime(user.created_at)}</td><td>${user.role === 'admin' ? '—' : `<button class="ghost user-toggle" data-id="${user.id}" data-status="${user.status === 1 ? 2 : 1}">${user.status === 1 ? '停用' : '启用'}</button><button class="ghost user-password" data-id="${user.id}" data-username="${escapeAttr(user.username)}">重置并复制</button>`}</td></tr>`).join('')
    $('#uploads-list').innerHTML = uploads.length ? uploads.map((item) => `<div class="audit-item ${item.success ? 'success' : 'failed'}"><b>${escapeHTML(item.channel_name)} · ${item.success ? '成功' : '失败'}</b><span>${escapeHTML(item.username)} / 类型 ${item.channel_type} / ${formatTime(item.created_at)}</span><span>${escapeHTML(item.message)}</span></div>`).join('') : '<div class="audit-item"><span>暂无记录</span></div>'
    renderAdminSettings(settings)
    $$('.user-toggle').forEach((button) => button.addEventListener('click', () => updateUser(button.dataset.id, { status: Number(button.dataset.status) })))
    $$('.user-password').forEach((button) => button.addEventListener('click', () => resetPassword(button.dataset.id, button.dataset.username)))
  } catch (err) { toast(err.message, true) }
}

function renderAdminSettings(settings) {
  const form = $('#newapi-settings-form')
  form.elements.newapi_base_url.value = settings.newapi_base_url || ''
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

async function resetPassword(id, username) {
  if (!confirm(`确定重置 ${username} 的密码？旧密码将立即失效。`)) return
  const password = randomPassword()
  try {
    await api(`/api/admin/users/${id}`, { method: 'PATCH', body: JSON.stringify({ password }) })
    showCredential(username, password)
    toast('密码已重置，可立即复制')
    await loadAdmin()
  } catch (err) { toast(err.message, true) }
}

function setupChannelTypes() {
  $('#channel-type').innerHTML = platform.channel_types.map((item) => `<option value="${item.id}">${item.id} · ${escapeHTML(item.name)}</option>`).join('')
  updateBasePolicy()
}

$('#channel-type').addEventListener('change', updateBasePolicy)
function updateBasePolicy() {
  const channelType = Number($('#channel-type').value)
  const anthropic = channelType === 14
  const baseURLSelect = $('#anthropic-base-url')
  baseURLSelect.disabled = !anthropic
  if (!anthropic) baseURLSelect.value = ''
  $('#base-policy').textContent = anthropic ? '可选空或 OpenRouter' : '强制留空'
  $$('.provider-field[data-types]').forEach((field) => {
    const types = field.dataset.types.split(',').map(Number)
    field.classList.toggle('hidden', !types.includes(channelType))
  })
  $('#openai-organization-field').classList.toggle('hidden', channelType !== 1)
  const providerLabels = { 18: '模型版本 *', 21: '知识库 ID *', 39: 'Cloudflare Account ID *', 49: 'Coze Agent ID *' }
  $('#provider-other-label').textContent = providerLabels[channelType] || '附加配置'
}

async function loadMetadata() {
  if (!platform.newapi_configured) { $('#groups').innerHTML = '<span class="muted">管理员尚未配置 New API 连接</span>'; return }
  try {
    const metadata = await api('/api/newapi/metadata')
    channelMetadata = metadata
    $('#groups').innerHTML = metadata.groups.length ? metadata.groups.map((group, index) => `<label><input type="checkbox" name="group" value="${escapeAttr(group)}" ${index === 0 ? 'checked' : ''}><span>${escapeHTML(group)}</span></label>`).join('') : '<span class="muted">New API 暂无分组</span>'
    const allModels = normalizeModelList(metadata.all_models?.length ? metadata.all_models : metadata.models)
    $('#model-options').innerHTML = allModels.map((model) => `<option value="${escapeAttr(model)}"></option>`).join('')
    $('#prefill-groups').innerHTML = (metadata.prefill_groups || []).map((group) => `<button class="ghost prefill-models" type="button" data-items="${escapeAttr(JSON.stringify(group.items))}">+ ${escapeHTML(group.name)}</button>`).join('')
    $$('.prefill-models').forEach((button) => button.addEventListener('click', () => appendModels(parsePrefillItems(button.dataset.items))))
  } catch (err) { $('#groups').innerHTML = `<span class="muted">${escapeHTML(err.message)}</span>`; toast(err.message, true) }
}

function normalizeModelList(items) {
  return [...new Set((items || []).map((item) => typeof item === 'string' ? item : item?.id).filter(Boolean))]
}

function parsePrefillItems(raw) {
  try {
    const value = JSON.parse(raw)
    const items = typeof value === 'string' ? JSON.parse(value) : value
    return Array.isArray(items) ? items : []
  } catch { return [] }
}

function appendModels(models, replace = false) {
  const current = replace ? [] : $('#models').value.split(',')
  $('#models').value = [...new Set([...current, ...models].map((item) => String(item).trim()).filter(Boolean))].join(',')
}

$('#fill-all-models').addEventListener('click', () => appendModels(normalizeModelList(channelMetadata.models), true))
$('#fill-related-models').addEventListener('click', () => {
  const type = Number($('#channel-type').value)
  const patterns = { 1: /^(gpt-|o\d|text-|dall-e)/i, 14: /^claude/i, 24: /^gemini/i, 34: /^(command|embed)/i, 42: /^(mistral|codestral)/i, 43: /^deepseek/i, 48: /^grok/i }
  const all = normalizeModelList(channelMetadata.all_models?.length ? channelMetadata.all_models : channelMetadata.models)
  appendModels(patterns[type] ? all.filter((model) => patterns[type].test(model)) : all, true)
})
$('#clear-models').addEventListener('click', () => { $('#models').value = '' })
$('#copy-models').addEventListener('click', async () => { await copyText($('#models').value); toast('模型列表已复制') })
$('#fetch-upstream-models').addEventListener('click', async (event) => {
  const form = new FormData($('#channel-form'))
  const key = String(form.get('key') || '').trim()
  if (!key) { toast('请先填写渠道密钥', true); return }
  event.currentTarget.disabled = true
  try {
    const models = await api('/api/newapi/fetch-models', { method: 'POST', body: JSON.stringify({ type: Number(form.get('type')), key, base_url: String(form.get('base_url') || '') }) })
    appendModels(models, true)
    toast(`已获取 ${models.length} 个上游模型`)
  } catch (err) { toast(err.message, true) } finally { event.currentTarget.disabled = false }
})

const jsonFields = ['model_mapping', 'status_code_mapping', 'setting', 'param_override', 'header_override', 'settings']
function optionalJSON(form, field) {
  const raw = String(form.get(field) || '').trim()
  if (!raw) return undefined
  try { return JSON.stringify(JSON.parse(raw)) } catch { throw new Error(`${field} 不是有效 JSON`) }
}

function structuredJSON(formElement, form, rawField, attribute) {
  let result = {}
  const raw = String(form.get(rawField) || '').trim()
  if (raw) {
    result = JSON.parse(raw)
    if (!result || Array.isArray(result) || typeof result !== 'object') throw new Error(`${rawField} 必须是 JSON 对象`)
  }
  formElement.querySelectorAll(`[${attribute}]`).forEach((input) => {
    if (input.closest('.provider-field')?.classList.contains('hidden')) return
    const key = input.getAttribute(attribute)
    let value = input.type === 'checkbox' ? input.checked : input.value
    if (input.type === 'number') value = Number(value || 0)
    if (key === 'upstream_model_update_ignored_models') value = String(value).split(',').map((item) => item.trim()).filter(Boolean)
    result[key] = value
  })
  if (attribute === 'data-setting-key') {
    formElement.querySelectorAll('[data-setting-json-key]').forEach((input) => {
      const rawValue = input.value.trim()
      result[input.dataset.settingJsonKey] = rawValue ? JSON.parse(rawValue) : []
    })
  }
  return JSON.stringify(result)
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
      status: Number(form.get('status')), priority: Number(form.get('priority') || 0), weight: Number(form.get('weight') || 0), auto_ban: Number(form.get('auto_ban')), base_url: String(form.get('base_url') || ''),
    }
    for (const field of ['test_model', 'openai_organization', 'tag', 'remark']) {
      const value = String(form.get(field) || '').trim(); if (value) channel[field] = value
    }
    const providerOther = String(form.get('other') || '').trim()
    const rawOther = String(form.get('other_raw') || '').trim()
    if (providerOther || rawOther) channel.other = providerOther || rawOther
    for (const field of ['model_mapping', 'status_code_mapping', 'param_override', 'header_override']) { const value = optionalJSON(form, field); if (value !== undefined) channel[field] = value }
    const setting = JSON.parse(structuredJSON(event.currentTarget, form, 'setting', 'data-setting-key'))
    const settings = JSON.parse(structuredJSON(event.currentTarget, form, 'settings', 'data-settings-key'))
    const channelType = Number(form.get('type'))
    if (channelType === 3 && form.get('azure_responses_version')) settings.azure_responses_version = form.get('azure_responses_version')
    if (channelType === 20) settings.openrouter_enterprise = form.get('openrouter_enterprise') === 'true'
    if (channelType === 33) settings.aws_key_type = form.get('aws_key_type') || 'ak_sk'
    if (channelType === 41) settings.vertex_key_type = form.get('vertex_key_type') || 'json'
    if (channelType === 58 && String(form.get('advanced_custom') || '').trim()) settings.advanced_custom = JSON.parse(form.get('advanced_custom'))
    if ([14, 33].includes(channelType) && $('#web-search-key').value.trim()) settings.web_search_emulation = { enabled: $('#web-search-enabled').checked, type: $('#web-search-type').value, api_key: $('#web-search-key').value.trim(), use_channel_proxy: $('#web-search-proxy').checked, max_results: Number($('#web-search-max').value || 5) }
    channel.setting = JSON.stringify(setting)
    channel.settings = JSON.stringify(settings)
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

function randomPassword(length = 20) {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_'
  const random = new Uint32Array(length)
  crypto.getRandomValues(random)
  return [...random].map((value) => alphabet[value % alphabet.length]).join('')
}

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value)
    return
  }
  const input = document.createElement('textarea')
  input.value = value
  input.style.position = 'fixed'
  input.style.opacity = '0'
  document.body.appendChild(input)
  input.select()
  document.execCommand('copy')
  input.remove()
}

function showCredential(username, password) {
  $('#credential-username').value = username
  $('#credential-password').value = password
  $('#credential-dialog').showModal()
}

$('#close-credential-dialog').addEventListener('click', () => $('#credential-dialog').close())
$('#copy-password').addEventListener('click', async () => {
  await copyText($('#credential-password').value)
  toast('密码已复制')
})
$('#copy-credential').addEventListener('click', async () => {
  await copyText(`用户名：${$('#credential-username').value}\n密码：${$('#credential-password').value}`)
  toast('账号和密码已复制')
})

function escapeHTML(value) { const node = document.createElement('div'); node.textContent = String(value ?? ''); return node.innerHTML }
function escapeAttr(value) { return escapeHTML(value).replaceAll('"', '&quot;') }
function formatTime(value) { try { return new Date(value).toLocaleString('zh-CN', { hour12: false }) } catch { return value } }

;(async () => {
  try { await enterApp(await api('/api/me')) } catch { showLogin() }
})()
