/**
 * SendFile - Local Wi-Fi File Transfer Client App
 */

// Application State
const state = {
  myInfo: null,
  myClientId: null,
  peers: [],
  selectedPeer: null,
  stagedFiles: [],
  transfers: {},
  pendingRequest: null,
  currentOffer: null,
};

// DOM Elements
const el = {
  myDeviceName: document.getElementById('my-device-name'),
  myDeviceIp: document.getElementById('my-device-ip'),
  myDeviceIcon: document.getElementById('my-device-icon'),
  peerCount: document.getElementById('peer-count'),
  peersContainer: document.getElementById('peers-container'),
  selectedTargetHint: document.getElementById('selected-target-hint'),
  dropzone: document.getElementById('dropzone'),
  fileInput: document.getElementById('file-input'),
  stagedFilesList: document.getElementById('staged-files-list'),
  btnSendFiles: document.getElementById('btn-send-files'),
  btnClearFiles: document.getElementById('btn-clear-files'),
  transfersList: document.getElementById('transfers-list'),
  activeTransferCount: document.getElementById('active-transfer-count'),
  btnRefresh: document.getElementById('btn-refresh'),
  btnQr: document.getElementById('btn-qr'),
  btnSettings: document.getElementById('btn-settings'),
  btnOpenFolder: document.getElementById('btn-open-folder'),
  saveLocationSubtitle: document.getElementById('save-location-subtitle'),
  // Incoming Modal
  modalIncoming: document.getElementById('modal-incoming'),
  incomingSenderName: document.getElementById('incoming-sender-name'),
  incomingTotalSize: document.getElementById('incoming-total-size'),
  incomingFileCount: document.getElementById('incoming-file-count'),
  incomingFilesPreview: document.getElementById('incoming-files-preview'),
  btnAcceptTransfer: document.getElementById('btn-accept-transfer'),
  btnDeclineTransfer: document.getElementById('btn-decline-transfer'),
  // Settings Modal
  modalSettings: document.getElementById('modal-settings'),
  settingName: document.getElementById('setting-name'),
  settingSaveDir: document.getElementById('setting-savedir'),
  settingAutoAccept: document.getElementById('setting-auto-accept'),
  btnSaveSettings: document.getElementById('btn-save-settings'),
  btnCloseSettings: document.getElementById('btn-close-settings'),
  // QR Modal
  modalQr: document.getElementById('modal-qr'),
  qrCodeImg: document.getElementById('qr-code-img'),
  qrUrlText: document.getElementById('qr-url-text'),
  btnCopyUrl: document.getElementById('btn-copy-url'),
  btnCloseQr: document.getElementById('btn-close-qr'),
  // Download Offer Modal (Mobile receiving from PC)
  modalDownloadOffer: document.getElementById('modal-download-offer'),
  downloadOfferSubtitle: document.getElementById('download-offer-subtitle'),
  downloadOfferTotalSize: document.getElementById('download-offer-total-size'),
  downloadOfferFileCount: document.getElementById('download-offer-file-count'),
  downloadOfferFilesList: document.getElementById('download-offer-files-list'),
  btnStartDownloads: document.getElementById('btn-start-downloads'),
  btnCloseDownloadOffer: document.getElementById('btn-close-download-offer'),
};

// Check if running on remote/guest browser (e.g. phone scanning QR)
function isGuestClient() {
  const host = window.location.hostname;
  return host !== 'localhost' && host !== '127.0.0.1';
}

function getClientDeviceDetails() {
  const ua = navigator.userAgent;
  let name = localStorage.getItem('sendfile_custom_name');
  let os = 'web';
  let devType = 'desktop';

  if (/iPhone/i.test(ua)) {
    if (!name) name = 'Apple iPhone';
    os = 'iOS';
    devType = 'mobile';
  } else if (/iPad/i.test(ua)) {
    if (!name) name = 'Apple iPad';
    os = 'iOS';
    devType = 'mobile';
  } else if (/Android/i.test(ua)) {
    if (!name) name = 'Android Phone';
    os = 'Android';
    devType = 'mobile';
  } else if (/Mac/i.test(ua)) {
    if (!name) name = 'Mac Device';
    os = 'macOS';
  } else if (/Windows/i.test(ua)) {
    if (!name) name = 'Windows PC';
    os = 'Windows';
  } else {
    if (!name) name = 'Mobile Device';
    devType = 'mobile';
  }

  return { name, os, deviceType: devType };
}

async function registerWebClient() {
  if (!isGuestClient()) return;
  const details = getClientDeviceDetails();
  try {
    await fetch('/api/peers/register-web', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        client_id: state.myClientId,
        name: details.name,
        device_type: details.deviceType,
        os: details.os,
      }),
    });
  } catch (e) {
    // Silent fail if network unreachable
  }
}

// Mobile Segmented Tab Switcher
window.switchMobileTab = function(tab) {
  const leftPanel = document.querySelector('.left-panel');
  const rightPanel = document.querySelector('.right-panel');
  const tabBtnSend = document.getElementById('tab-btn-send');
  const tabBtnActivity = document.getElementById('tab-btn-activity');

  if (tab === 'send') {
    if (tabBtnSend) tabBtnSend.classList.add('active');
    if (tabBtnActivity) tabBtnActivity.classList.remove('active');
    if (leftPanel) leftPanel.classList.remove('mobile-hidden');
    if (rightPanel) rightPanel.classList.add('mobile-hidden');
  } else {
    if (tabBtnActivity) tabBtnActivity.classList.add('active');
    if (tabBtnSend) tabBtnSend.classList.remove('active');
    if (leftPanel) leftPanel.classList.add('mobile-hidden');
    if (rightPanel) rightPanel.classList.remove('mobile-hidden');
  }
};

// Initialize
document.addEventListener('DOMContentLoaded', async () => {
  // Generate or retrieve persistent client ID
  let clientId = localStorage.getItem('sendfile_client_id');
  if (!clientId) {
    clientId = 'web_' + Math.random().toString(16).substring(2, 10);
    localStorage.setItem('sendfile_client_id', clientId);
  }
  state.myClientId = clientId;

  setupEventListeners();
  await loadMyInfo();
  await registerWebClient();
  await refreshPeers();
  await loadTransfers();
  initSSE();

  // Mobile layout initialization
  if (window.innerWidth <= 768) {
    switchMobileTab('send');
  }
  if (isGuestClient() && el.btnQr) {
    el.btnQr.style.display = 'none'; // Phone doesn't need to scan its own screen
  }

  // Periodic polling fallback & client heartbeats
  setInterval(refreshPeers, 3000);
  setInterval(loadTransfers, 2000);

  if (isGuestClient()) {
    setInterval(registerWebClient, 2500);
    window.addEventListener('beforeunload', () => {
      navigator.sendBeacon('/api/peers/unregister-web', JSON.stringify({ client_id: state.myClientId }));
    });
  }
});

// Event Listeners Setup
function setupEventListeners() {
  // Refresh button
  el.btnRefresh.addEventListener('click', async () => {
    el.btnRefresh.style.transform = 'rotate(180deg)';
    await registerWebClient();
    await fetch('/api/peers/refresh', { method: 'POST' });
    await refreshPeers();
    setTimeout(() => { el.btnRefresh.style.transform = 'none'; }, 400);
  });

  // Settings Modal
  el.btnSettings.addEventListener('click', () => {
    if (isGuestClient()) {
      const details = getClientDeviceDetails();
      el.settingName.value = details.name;
      el.settingSaveDir.value = 'Browser Downloads (Phone)';
      el.settingSaveDir.disabled = true;
      el.settingAutoAccept.disabled = false;
      const localPref = localStorage.getItem('sendfile_auto_accept');
      if (localPref !== null) {
        el.settingAutoAccept.checked = localPref === 'true';
      } else if (state.myInfo) {
        el.settingAutoAccept.checked = !!state.myInfo.auto_accept;
      } else {
        el.settingAutoAccept.checked = false;
      }
    } else if (state.myInfo) {
      el.settingName.value = state.myInfo.device_name || '';
      el.settingSaveDir.value = state.myInfo.save_dir || '';
      el.settingSaveDir.disabled = false;
      el.settingAutoAccept.checked = !!state.myInfo.auto_accept;
      el.settingAutoAccept.disabled = false;
    }
    el.modalSettings.classList.add('open');
  });

  el.btnCloseSettings.addEventListener('click', () => {
    el.modalSettings.classList.remove('open');
  });

  el.btnSaveSettings.addEventListener('click', saveSettings);

  // QR Modal
  el.btnQr.addEventListener('click', openQRModal);
  el.btnCloseQr.addEventListener('click', () => el.modalQr.classList.remove('open'));
  el.btnCopyUrl.addEventListener('click', () => {
    const url = el.qrUrlText.innerText;
    navigator.clipboard.writeText(url).then(() => {
      alert('Address copied to clipboard!');
    });
  });

  // Dropzone and File Picker
  el.dropzone.addEventListener('click', () => el.fileInput.click());
  el.fileInput.addEventListener('change', handleFilesSelected);

  el.dropzone.addEventListener('dragover', (e) => {
    e.preventDefault();
    el.dropzone.classList.add('drag-active');
  });

  el.dropzone.addEventListener('dragleave', () => {
    el.dropzone.classList.remove('drag-active');
  });

  el.dropzone.addEventListener('drop', (e) => {
    e.preventDefault();
    el.dropzone.classList.remove('drag-active');
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      addStagedFiles(Array.from(e.dataTransfer.files));
    }
  });

  // Send & Clear Buttons
  el.btnSendFiles.addEventListener('click', sendStagedFiles);
  el.btnClearFiles.addEventListener('click', clearStagedFiles);

  // Incoming transfer decision (Accept/Decline)
  el.btnAcceptTransfer.addEventListener('click', () => handleIncomingDecision(true));
  el.btnDeclineTransfer.addEventListener('click', () => handleIncomingDecision(false));

  // Download Offer Modal Buttons (For phone receiving files)
  if (el.btnStartDownloads) {
    el.btnStartDownloads.addEventListener('click', () => {
      if (!state.currentOffer) return;
      window.triggerAllDownloads(state.currentOffer.files, state.currentOffer.transfer_id);
      el.modalDownloadOffer.classList.remove('open');
      state.currentOffer = null;
    });
  }

  if (el.btnCloseDownloadOffer) {
    el.btnCloseDownloadOffer.addEventListener('click', () => {
      if (el.modalDownloadOffer) el.modalDownloadOffer.classList.remove('open');
      state.currentOffer = null;
    });
  }

  // Open Folder button
  if (el.btnOpenFolder) {
    el.btnOpenFolder.addEventListener('click', async () => {
      try {
        await fetch('/api/open-folder', { method: 'POST' });
      } catch (err) {
        console.error('Failed to open folder:', err);
      }
    });
  }
}

// Global openFolder function for transfer cards
window.openFolder = async function() {
  try {
    await fetch('/api/open-folder', { method: 'POST' });
  } catch (err) {
    console.error('Failed to open folder:', err);
  }
};

// Fetch Node Info
async function loadMyInfo() {
  try {
    const res = await fetch('/api/info');
    if (!res.ok) return;
    const info = await res.json();
    state.myInfo = info;

    if (isGuestClient()) {
      const details = getClientDeviceDetails();
      el.myDeviceName.innerText = details.name;
      el.myDeviceIp.innerText = `Connected to: ${info.device_name}`;
      el.myDeviceIcon.innerText = getDeviceEmoji(details.deviceType, details.os);
      if (el.saveLocationSubtitle) el.saveLocationSubtitle.innerText = 'Transfers save to your phone downloads';
      if (el.btnOpenFolder) el.btnOpenFolder.style.display = 'none';
    } else {
      el.myDeviceName.innerText = info.device_name || 'My Device';
      el.myDeviceIp.innerText = `${info.primary_ip}:${info.http_port}`;
      el.myDeviceIcon.innerText = getDeviceEmoji(info.device_type, info.os);
      if (el.saveLocationSubtitle) el.saveLocationSubtitle.innerText = `Saved in: ${info.save_dir || 'Downloads/SendFile'}`;
      if (el.btnOpenFolder) el.btnOpenFolder.style.display = 'inline-flex';
    }
  } catch (err) {
    console.error('Failed to load info:', err);
  }
}

// Fetch Discovered Peers
async function refreshPeers() {
  try {
    const res = await fetch('/api/peers');
    if (!res.ok) return;
    const peers = await res.json();
    state.peers = peers || [];
    renderPeers();
  } catch (err) {
    console.error('Failed to refresh peers:', err);
  }
}

// Render Peer Cards
function renderPeers() {
  // Filter out self
  let displayPeers = state.peers.filter(p => p.id !== state.myClientId);

  // If viewing from phone over Wi-Fi, add Host PC as a primary option
  if (isGuestClient() && state.myInfo) {
    const hostPeer = {
      id: 'host_node',
      name: `${state.myInfo.device_name} (Host PC)`,
      device_type: state.myInfo.device_type,
      os: state.myInfo.os,
      ip: state.myInfo.primary_ip,
      port: state.myInfo.http_port,
      isHost: true,
    };
    displayPeers.unshift(hostPeer);

    // Auto-select host by default if nothing selected
    if (!state.selectedPeer) {
      state.selectedPeer = hostPeer;
      el.selectedTargetHint.innerText = `Sending to: ${hostPeer.name}`;
    }
  }

  el.peerCount.innerText = `${displayPeers.length} available`;

  if (displayPeers.length === 0) {
    el.peersContainer.innerHTML = `
      <div class="empty-peers">
        <div class="radar-scan"></div>
        <p>Scanning local Wi-Fi for devices...</p>
        <span class="hint">Ensure other devices are on the same Wi-Fi with SendFile running, or scan the QR code to connect from phone.</span>
      </div>
    `;
    updateSendButtonState();
    return;
  }

  el.peersContainer.innerHTML = displayPeers.map(peer => {
    const isSelected = state.selectedPeer && state.selectedPeer.id === peer.id;
    const emoji = getDeviceEmoji(peer.device_type, peer.os);
    let tag = peer.isHost ? 'HOST' : (peer.is_web ? '📱 PHONE' : (peer.os || 'LAN'));
    return `
      <div class="peer-card ${isSelected ? 'selected' : ''}" onclick="selectPeer('${peer.id}')">
        <div class="peer-avatar">${emoji}</div>
        <div class="peer-name" title="${escapeHtml(peer.name)}">${escapeHtml(peer.name)}</div>
        <div class="peer-meta">
          <span class="os-tag">${escapeHtml(tag)}</span>
          <span class="peer-ip">${escapeHtml(peer.ip)}${peer.is_web ? '' : ':' + peer.port}</span>
        </div>
      </div>
    `;
  }).join('');

  updateSendButtonState();
}

window.selectPeer = function(peerId) {
  let peer = state.peers.find(p => p.id === peerId);
  if (!peer && peerId === 'host_node' && state.myInfo) {
    peer = {
      id: 'host_node',
      name: `${state.myInfo.device_name} (Host PC)`,
      device_type: state.myInfo.device_type,
      os: state.myInfo.os,
      ip: state.myInfo.primary_ip,
      port: state.myInfo.http_port,
      isHost: true,
    };
  }
  if (!peer) return;

  if (state.selectedPeer && state.selectedPeer.id === peerId) {
    state.selectedPeer = null; // Toggle off
    el.selectedTargetHint.innerText = 'Select a device above to send files';
  } else {
    state.selectedPeer = peer;
    el.selectedTargetHint.innerText = `Sending to: ${peer.name} (${peer.ip})`;
  }

  renderPeers();
};

// Handle File Selection
function handleFilesSelected(e) {
  if (e.target.files && e.target.files.length > 0) {
    addStagedFiles(Array.from(e.target.files));
    el.fileInput.value = '';
  }
}

function addStagedFiles(newFiles) {
  for (const f of newFiles) {
    state.stagedFiles.push(f);
  }
  renderStagedFiles();
}

function renderStagedFiles() {
  if (state.stagedFiles.length === 0) {
    el.stagedFilesList.innerHTML = '';
    el.btnClearFiles.style.display = 'none';
    updateSendButtonState();
    return;
  }

  el.btnClearFiles.style.display = 'inline-block';
  el.stagedFilesList.innerHTML = state.stagedFiles.map((f, idx) => `
    <div class="staged-file-item">
      <div class="file-info-group">
        <span class="file-name-text" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span>
        <span class="file-size-text">${formatBytes(f.size)}</span>
      </div>
      <button class="btn-remove-file" onclick="removeStagedFile(${idx})">&times;</button>
    </div>
  `).join('');

  updateSendButtonState();
}

window.removeStagedFile = function(index) {
  state.stagedFiles.splice(index, 1);
  renderStagedFiles();
};

function clearStagedFiles() {
  state.stagedFiles = [];
  if (el.fileInput) {
    el.fileInput.value = '';
  }
  renderStagedFiles();
}

function updateSendButtonState() {
  const canSend = state.selectedPeer && state.stagedFiles.length > 0;
  el.btnSendFiles.disabled = !canSend;
  if (!state.selectedPeer) {
    el.btnSendFiles.querySelector('span').innerText = 'Select Target Device';
  } else if (state.stagedFiles.length === 0) {
    el.btnSendFiles.querySelector('span').innerText = 'Select Files to Send';
  } else {
    el.btnSendFiles.querySelector('span').innerText = `Send ${state.stagedFiles.length} file(s) to ${state.selectedPeer.name}`;
  }
}

// Send Staged Files to Selected Peer
async function sendStagedFiles() {
  if (!state.selectedPeer || state.stagedFiles.length === 0) return;

  const target = state.selectedPeer;
  const files = [...state.stagedFiles];
  const transferId = 'tx_' + Date.now();

  el.btnSendFiles.disabled = true;

  // Phase 1: Instant lightweight handshake (Metadata only, < 10ms)
  // Sends tiny JSON so Accept/Decline pops up on the destination INSTANTLY
  // without waiting to upload gigabytes of file data first!
  if (target.isHost) {
    el.btnSendFiles.querySelector('span').innerText = 'Asking recipient to accept...';
    const fileItems = files.map((f, i) => ({
      index: i,
      name: f.name,
      size: f.size,
      type: f.type || 'application/octet-stream',
    }));
    const totalBytes = files.reduce((acc, f) => acc + f.size, 0);

    try {
      const handshakeRes = await fetch('/api/transfer/request', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          transfer_id: transferId,
          sender_id: state.myClientId,
          sender_name: getClientDeviceDetails().name,
          sender_os: getClientDeviceDetails().os,
          files: fileItems,
          total_bytes: totalBytes,
        }),
      });

      if (!handshakeRes.ok) {
        throw new Error('Handshake failed');
      }

      const decision = await handshakeRes.json();
      if (!decision.accepted) {
        alert('Transfer was declined by recipient');
        updateSendButtonState();
        return;
      }
    } catch (err) {
      alert(`Handshake error: ${err.message}`);
      updateSendButtonState();
      return;
    }
  }

  // Auto clear staged files immediately as transfer is initiated/accepted
  clearStagedFiles();

  // On mobile devices, switch tab to activity so user immediately sees live progress
  if (window.innerWidth <= 768) {
    switchMobileTab('activity');
  }

  el.btnSendFiles.disabled = true;
  el.btnSendFiles.querySelector('span').innerText = 'Streaming files...';

  const formData = new FormData();
  formData.append('client_id', state.myClientId);
  formData.append('transfer_id', transferId);
  for (const f of files) {
    formData.append('files', f);
  }

  try {
    let endpoint = '/api/send-direct';
    if (target.isHost) {
      // Direct guest upload to this host PC
      endpoint = '/api/direct-upload';
    } else {
      formData.append('target_peer_id', target.id);
      formData.append('target_ip', target.ip);
      formData.append('target_port', (target.port || 53318).toString());
    }

    const res = await fetch(endpoint, {
      method: 'POST',
      body: formData,
    });

    const data = await res.json();
    if (!res.ok) {
      alert(`Transfer failed: ${data.error || 'Server error'}`);
      return;
    }

    await loadTransfers();
    if (target.isHost) {
      alert('Files successfully sent to Host PC!');
    }
  } catch (err) {
    alert(`Transfer error: ${err.message}`);
  } finally {
    updateSendButtonState();
  }
}

// Load Transfers and Activity
async function loadTransfers() {
  try {
    const res = await fetch('/api/transfers');
    if (!res.ok) return;
    const list = await res.json();
    if (!list) return;

    let activeCount = 0;
    for (const session of list) {
      state.transfers[session.id] = session;
      if (session.status === 'transferring' || session.status === 'pending') {
        activeCount++;
      }
      if (session.direction === 'receive' && session.status === 'pending') {
        if (!isGuestClient() && session.peer_id !== state.myClientId) {
          showIncomingPrompt(session);
        }
      }
    }

    el.activeTransferCount.innerText = activeCount > 0 ? `${activeCount} Active` : 'Idle';

    const mobileBadge = document.getElementById('mobile-activity-badge');
    if (mobileBadge) {
      if (activeCount > 0) {
        mobileBadge.innerText = activeCount;
        mobileBadge.style.display = 'inline-block';
      } else {
        mobileBadge.style.display = 'none';
      }
    }

    renderTransfers();
  } catch (err) {
    console.error('Failed to load transfers:', err);
  }
}

function renderTransfers() {
  const sessions = Object.values(state.transfers).sort((a, b) => new Date(b.created_at) - new Date(a.created_at));

  if (sessions.length === 0) {
    el.transfersList.innerHTML = `
      <div class="empty-transfers">
        <svg viewBox="0 0 24 24" width="40" height="40" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/>
          <polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
          <line x1="12" y1="22.08" x2="12" y2="12"/>
        </svg>
        <p>No active transfers</p>
        <span>Transferred files are saved directly to your Downloads folder</span>
      </div>
    `;
    return;
  }

  el.transfersList.innerHTML = sessions.map(s => {
    const percent = s.total_bytes > 0 ? Math.min(100, Math.round((s.transferred_bytes / s.total_bytes) * 100)) : (s.status === 'completed' ? 100 : 0);
    const speed = s.speed_bytes_per_sec > 0 ? `${formatBytes(s.speed_bytes_per_sec)}/s` : '';
    const eta = s.eta_seconds > 0 ? `• ~${s.eta_seconds}s left` : '';
    const directionIcon = s.direction === 'send' ? '↑' : '↓';
    const peerLabel = s.peer_name ? `${s.peer_name} (${s.peer_ip})` : s.peer_ip;
    const fileNames = s.files ? s.files.map(f => f.name).join(', ') : 'File';
    const isCompleted = s.status === 'completed';
    const folderAction = (!isGuestClient() && isCompleted) ?
      `<button class="icon-btn small" onclick="openFolder()" title="Open Folder in File Explorer" style="margin-left:8px; padding: 2px 6px; font-size: 0.75rem; width: auto; height: 24px; display: inline-flex;">📁 Open</button>` : '';

    let filesHtml = '';
    if (isGuestClient() && s.files && s.files.length > 0) {
      filesHtml = `
        <div style="display: flex; flex-direction: column; gap: 6px; margin: 8px 0; border-top: 1px solid rgba(255,255,255,0.08); padding-top: 8px;">
          ${s.files.map((f, fIdx) => `
            <div style="display: flex; align-items: center; justify-content: space-between; background: rgba(0,0,0,0.25); padding: 8px 10px; border-radius: var(--radius-sm); font-size: 0.8rem;">
              <div style="overflow: hidden; max-width: 65%;">
                <div style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500;">${escapeHtml(f.name)}</div>
                <div style="font-size: 0.7rem; color: var(--text-muted);">${formatBytes(f.size)}</div>
              </div>
              <button class="primary-btn small" onclick="window.triggerDownload('${s.id}', ${fIdx}, '${escapeHtml(f.name)}')" style="padding: 4px 10px; font-size: 0.75rem; height: 28px; width: auto;">
                ⬇️ Download
              </button>
            </div>
          `).join('')}
        </div>
      `;
    }

    return `
      <div class="transfer-card">
        <div class="transfer-header">
          <div class="transfer-title">
            <span>${directionIcon}</span>
            <span title="${escapeHtml(fileNames)}">${escapeHtml(fileNames)}</span>
          </div>
          <span class="transfer-badge ${s.status}">${s.status}</span>
        </div>

        <div class="progress-track">
          <div class="progress-fill ${s.status === 'transferring' ? 'active' : ''}" style="width: ${percent}%;"></div>
        </div>

        ${filesHtml}

        <div class="transfer-footer">
          <div style="display: flex; align-items: center; flex-wrap: wrap;">
            <span>${formatBytes(s.transferred_bytes)} of ${formatBytes(s.total_bytes)} (${percent}%)</span>
            <span style="margin: 0 4px;">•</span>
            <span>${peerLabel}</span>
            ${folderAction}
          </div>
          <div>
            <strong>${speed}</strong> ${eta}
          </div>
        </div>
      </div>
    `;
  }).join('');
}

// Prompt for Incoming Transfer
function showIncomingPrompt(session) {
  // If this device is a guest client (phone connected via browser), NEVER show host accept/decline dialog!
  if (isGuestClient()) return;

  // If this session was initiated by this client, don't ask ourselves to accept!
  if (session.peer_id === state.myClientId) return;

  if (state.pendingRequest && state.pendingRequest.id === session.id) return;
  state.pendingRequest = session;

  el.incomingSenderName.innerText = session.peer_name || session.peer_ip;
  el.incomingTotalSize.innerText = formatBytes(session.total_bytes);
  el.incomingFileCount.innerText = `${session.files ? session.files.length : 1} file(s)`;

  if (session.files) {
    el.incomingFilesPreview.innerHTML = session.files.map(f => `
      <div class="info-row">
        <span>${escapeHtml(f.name)}</span>
        <span>${formatBytes(f.size)}</span>
      </div>
    `).join('');
  }

  el.modalIncoming.classList.add('open');
}

async function handleIncomingDecision(accepted) {
  if (!state.pendingRequest) return;
  const sessionID = state.pendingRequest.id;

  try {
    await fetch('/api/transfer/decision', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ transfer_id: sessionID, accepted }),
    });
  } catch (err) {
    console.error('Failed to submit decision:', err);
  } finally {
    el.modalIncoming.classList.remove('open');
    state.pendingRequest = null;
    await loadTransfers();
  }
}

// Trigger single file download
window.triggerDownload = function(transferId, fileIndex, fileName) {
  const link = document.createElement('a');
  link.href = `/api/download?transfer_id=${encodeURIComponent(transferId)}&file_index=${fileIndex}`;
  link.download = fileName;
  link.target = '_blank';
  document.body.appendChild(link);
  link.click();
  setTimeout(() => {
    if (link.parentNode) link.parentNode.removeChild(link);
  }, 1000);
};

// Trigger multiple file downloads with stagger to prevent mobile browsers from discarding
window.triggerAllDownloads = function(files, transferId) {
  files.forEach((file, index) => {
    setTimeout(() => {
      window.triggerDownload(transferId, index, file.name);
    }, index * 1200); // 1.2s stagger between downloads
  });
};

// Download Offer from PC (Shown on mobile browser)
function showDownloadOfferModal(offer) {
  state.currentOffer = offer;
  const isAutoAccept = localStorage.getItem('sendfile_auto_accept') === 'true';

  // If mobile auto-accept is enabled, automatically trigger staggered downloads!
  if (isAutoAccept) {
    window.triggerAllDownloads(offer.files, offer.transfer_id);
    return;
  }

  if (el.downloadOfferSubtitle) {
    el.downloadOfferSubtitle.innerText = `${offer.sender_name} sent you files!`;
  }
  if (el.downloadOfferTotalSize) {
    el.downloadOfferTotalSize.innerText = formatBytes(offer.total_bytes);
  }
  if (el.downloadOfferFileCount) {
    el.downloadOfferFileCount.innerText = `${offer.files.length} file(s)`;
  }
  if (el.downloadOfferFilesList) {
    el.downloadOfferFilesList.innerHTML = offer.files.map((f, idx) => `
      <div class="info-row" style="align-items: center; padding: 8px 0; border-bottom: 1px solid rgba(255,255,255,0.08);">
        <div style="overflow: hidden; max-width: 65%;">
          <div style="font-weight: 500; text-overflow: ellipsis; overflow: hidden; white-space: nowrap;">${escapeHtml(f.name)}</div>
          <div style="font-size: 0.75rem; color: var(--text-muted);">${formatBytes(f.size)}</div>
        </div>
        <button class="primary-btn small" onclick="window.triggerDownload('${escapeHtml(offer.transfer_id)}', ${idx}, '${escapeHtml(f.name)}')" style="padding: 6px 12px; font-size: 0.8rem; width: auto; height: 32px;">
          ⬇️ Download
        </button>
      </div>
    `).join('');
  }
  if (el.btnStartDownloads) {
    el.btnStartDownloads.innerText = `Save All Files (${offer.files.length})`;
  }
  if (el.modalDownloadOffer) {
    el.modalDownloadOffer.classList.add('open');
  }
}

// Settings Handlers
async function saveSettings() {
  const name = el.settingName.value.trim();
  const autoAccept = el.settingAutoAccept.checked;

  if (isGuestClient()) {
    if (name) {
      localStorage.setItem('sendfile_custom_name', name);
    }
    localStorage.setItem('sendfile_auto_accept', autoAccept);

    // Sync auto_accept setting to host PC so transfers from phone don't require walking to the PC!
    try {
      await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ auto_accept: autoAccept }),
      });
    } catch (e) {
      console.error('Failed to sync settings:', e);
    }

    await registerWebClient();
    await loadMyInfo();
    el.modalSettings.classList.remove('open');
    return;
  }

  const saveDir = el.settingSaveDir.value.trim();

  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        device_name: name,
        save_dir: saveDir,
        auto_accept: autoAccept,
      }),
    });

    if (res.ok) {
      await loadMyInfo();
      el.modalSettings.classList.remove('open');
    } else {
      alert('Failed to update settings');
    }
  } catch (err) {
    alert(`Error: ${err.message}`);
  }
}

// QR Code Modal
function openQRModal() {
  if (!state.myInfo) return;
  const ip = state.myInfo.primary_ip || '127.0.0.1';
  const port = state.myInfo.http_port || 53318;
  const url = `http://${ip}:${port}`;

  el.qrUrlText.innerText = url;
  el.qrCodeImg.src = `https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=${encodeURIComponent(url)}`;
  el.modalQr.classList.add('open');
}

// Server-Sent Events for Real-Time Streaming
function initSSE() {
  const es = new EventSource('/api/events');

  es.addEventListener('transfer_update', (e) => {
    try {
      const session = JSON.parse(e.data);
      state.transfers[session.id] = session;
      renderTransfers();

      if (session.direction === 'receive' && session.status === 'pending') {
        if (!isGuestClient() && session.peer_id !== state.myClientId) {
          showIncomingPrompt(session);
        }
      }
    } catch (err) {
      console.error('SSE transfer parse error:', err);
    }
  });

  es.addEventListener('peer_list', (e) => {
    try {
      const peers = JSON.parse(e.data);
      state.peers = peers || [];
      renderPeers();
    } catch (err) {
      console.error('SSE peer parse error:', err);
    }
  });

  es.addEventListener('web_download_offer', (e) => {
    try {
      const offer = JSON.parse(e.data);
      if (offer.target_client_id === state.myClientId) {
        showDownloadOfferModal(offer);
      }
    } catch (err) {
      console.error('SSE web download offer parse error:', err);
    }
  });

  es.onerror = () => {
    // SSE disconnected, will auto reconnect
  };
}

// Helpers
function getDeviceEmoji(type, os) {
  const o = (os || '').toLowerCase();
  const t = (type || '').toLowerCase();

  if (o.includes('windows')) return '💻';
  if (o.includes('darwin') || o.includes('mac')) return '🍎';
  if (o.includes('ios') || o.includes('iphone') || o.includes('ipad')) return '📱';
  if (o.includes('android')) return '📱';
  if (o.includes('linux')) return '🐧';
  if (t === 'mobile') return '📱';
  return '💻';
}

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}
