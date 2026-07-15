package panel

const panelRuntimeStatsActionsPlaceholder = "__VEIL_PANEL_RUNTIME_STATS_ACTIONS__"

func panelRuntimeStatsActionsJS() string {
	return `    function finiteTelemetryNumber(value, fallback) {
      const number = Number(value);
      return Number.isFinite(number) ? number : (fallback || 0);
    }

    // Helper formats
    function formatBytesJS(bytes) {
      const value = Math.max(0, finiteTelemetryNumber(bytes, 0));
      if (value === 0) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.min(sizes.length - 1, Math.floor(Math.log(value) / Math.log(k)));
      return parseFloat((value / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    function formatDurationJS(seconds) {
      const value = Math.max(0, finiteTelemetryNumber(seconds, 0));
      if (!value) return '0s';
      const d = Math.floor(value / (3600*24));
      const h = Math.floor((value % (3600*24)) / 3600);
      const m = Math.floor((value % 3600) / 60);
      const s = Math.floor(value % 60);
      const parts = [];
      if (d > 0) parts.push(d + 'd');
      if (h > 0) parts.push(h + 'h');
      if (m > 0) parts.push(m + 'm');
      if (s > 0 || parts.length === 0) parts.push(s + 's');
      return parts.join(' ');
    }

    function appendTelemetryCell(row, text, className) {
      const cell = document.createElement('td');
      if (className) cell.className = className;
      cell.textContent = String(text === undefined || text === null ? '' : text);
      row.appendChild(cell);
      return cell;
    }

    window.toggleRawView = function(id) {
      const pre = document.getElementById(id);
      if (pre) pre.style.display = pre.style.display === 'none' ? 'block' : 'none';
    };

    function updateSystemTelemetry(stats) {
      stats = stats || {};
      const circ = 314;
      const cpuVal = finiteTelemetryNumber(stats.cpuPercent, 0);
      const cpuOffset = circ - (Math.min(100, Math.max(0, cpuVal)) / 100) * circ;
      document.getElementById('cpu-circle').style.strokeDashoffset = cpuOffset;
      document.getElementById('cpu-text').innerText = cpuVal.toFixed(1) + '%';
      const loadAvg1 = finiteTelemetryNumber(stats.loadAvg1, 0);
      const loadAvg5 = finiteTelemetryNumber(stats.loadAvg5, 0);
      const loadAvg15 = finiteTelemetryNumber(stats.loadAvg15, 0);
      document.getElementById('cpu-load-details').innerText = '1m/5m/15m: ' + loadAvg1.toFixed(2) + ' / ' + loadAvg5.toFixed(2) + ' / ' + loadAvg15.toFixed(2);

      const memTotal = Math.max(1, finiteTelemetryNumber(stats.memoryTotalMB, 1));
      const memUsed = Math.max(0, finiteTelemetryNumber(stats.memoryUsedMB, 0));
      const memPercent = (memUsed / memTotal) * 100;
      const memOffset = circ - (Math.min(100, Math.max(0, memPercent)) / 100) * circ;
      document.getElementById('mem-circle').style.strokeDashoffset = memOffset;
      document.getElementById('mem-text').innerText = memPercent.toFixed(1) + '%';
      document.getElementById('mem-usage-details').innerText = memUsed + ' MB / ' + memTotal + ' MB';

      const diskTotal = Math.max(1, finiteTelemetryNumber(stats.diskTotalGB, 1));
      const diskUsed = Math.max(0, finiteTelemetryNumber(stats.diskUsedGB, 0));
      const diskPercent = (diskUsed / diskTotal) * 100;
      const diskOffset = circ - (Math.min(100, Math.max(0, diskPercent)) / 100) * circ;
      document.getElementById('disk-circle').style.strokeDashoffset = diskOffset;
      document.getElementById('disk-text').innerText = diskPercent.toFixed(1) + '%';
      document.getElementById('disk-usage-details').innerText = diskUsed.toFixed(1) + ' GB / ' + diskTotal.toFixed(1) + ' GB';
    }

    document.getElementById('load-system-stats').addEventListener('click', async () => {
      const stats = await loadJSON('/api/system', 'system-stats-output');
      if (stats) updateSystemTelemetry(stats);
    });

    document.getElementById('load-network-stats').addEventListener('click', async () => {
      const net = await loadJSON('/api/network', 'network-stats-output');
      if (net && Array.isArray(net.interfaces)) {
        const tbody = document.getElementById('net-interfaces-tbody');
        tbody.textContent = '';
        net.interfaces.forEach((iface) => {
          const row = document.createElement('tr');
          const name = appendTelemetryCell(row, iface && iface.name);
          name.style.fontWeight = '600';
          const rx = appendTelemetryCell(row, '↓ ' + formatBytesJS(iface && iface.rxBytes));
          rx.style.color = '#34d399';
          const tx = appendTelemetryCell(row, '↑ ' + formatBytesJS(iface && iface.txBytes));
          tx.style.color = '#60a5fa';
          const rxPackets = finiteTelemetryNumber(iface && iface.rxPackets, 0).toLocaleString();
          const txPackets = finiteTelemetryNumber(iface && iface.txPackets, 0).toLocaleString();
          appendTelemetryCell(row, rxPackets + ' / ' + txPackets);
          tbody.appendChild(row);
        });
        document.getElementById('net-interfaces-table').style.display = 'block';
      }
    });

    document.getElementById('load-connections-stats').addEventListener('click', async () => {
      const conn = await loadJSON('/api/connections', 'connections-stats-output');
      if (conn && Array.isArray(conn.listeners)) {
        const container = document.getElementById('connections-list');
        container.textContent = '';
        if (conn.listeners.length === 0) {
          const empty = document.createElement('span');
          empty.style.color = 'var(--text-muted)';
          empty.textContent = 'No listening ports found.';
          container.appendChild(empty);
          return;
        }
        conn.listeners.forEach((listener) => {
          const proto = String(listener && listener.proto || '').toLowerCase();
          const badge = document.createElement('span');
          badge.className = 'badge';
          badge.style.background = proto === 'tcp' ? 'rgba(79, 70, 229, 0.15)' : 'rgba(245, 158, 11, 0.15)';
          badge.style.color = proto === 'tcp' ? '#818cf8' : '#fbbf24';
          badge.style.border = '1px solid ' + (proto === 'tcp' ? 'rgba(79, 70, 229, 0.3)' : 'rgba(245, 158, 11, 0.3)');
          badge.style.padding = '6px 12px';
          badge.style.borderRadius = '8px';
          badge.style.fontSize = '0.85rem';
          const protocol = document.createElement('strong');
          protocol.style.textTransform = 'uppercase';
          protocol.textContent = proto;
          badge.appendChild(protocol);
          badge.appendChild(document.createTextNode('  :' + finiteTelemetryNumber(listener && listener.port, 0)));
          if (listener && listener.process) {
            const process = document.createElement('span');
            process.style.opacity = '0.7';
            process.style.fontSize = '0.75rem';
            process.style.marginLeft = '6px';
            process.textContent = String(listener.process);
            badge.appendChild(process);
          }
          container.appendChild(badge);
        });
      }
    });

    document.getElementById('load-processes-stats').addEventListener('click', async () => {
      const proc = await loadJSON('/api/processes', 'processes-stats-output');
      if (proc && Array.isArray(proc.processes)) {
        const tbody = document.getElementById('processes-tbody');
        tbody.textContent = '';
        if (proc.processes.length === 0) {
          const row = document.createElement('tr');
          const cell = appendTelemetryCell(row, 'No managed processes running.');
          cell.colSpan = 5;
          cell.style.textAlign = 'center';
          cell.style.color = 'var(--text-muted)';
          tbody.appendChild(row);
          return;
        }
        proc.processes.forEach((process) => {
          const row = document.createElement('tr');
          appendTelemetryCell(row, finiteTelemetryNumber(process && process.pid, 0));
          const name = appendTelemetryCell(row, process && process.name);
          name.style.fontWeight = '600';
          appendTelemetryCell(row, finiteTelemetryNumber(process && process.cpuPercent, 0).toFixed(1) + '%');
          appendTelemetryCell(row, finiteTelemetryNumber(process && process.memoryMB, 0) + ' MB');
          appendTelemetryCell(row, formatDurationJS(process && process.uptimeSeconds));
          tbody.appendChild(row);
        });
        document.getElementById('processes-table').style.display = 'block';
      }
    });

    document.getElementById('load-disk-stats').addEventListener('click', async () => {
      const disk = await loadJSON('/api/disk', 'disk-stats-output');
      if (disk && Array.isArray(disk.dirs)) {
        const container = document.getElementById('disk-paths-container');
        container.textContent = '';
        let maxBytes = 1;
        disk.dirs.forEach((directory) => {
          maxBytes = Math.max(maxBytes, finiteTelemetryNumber(directory && directory.sizeBytes, 0));
        });
        disk.dirs.forEach((directory) => {
          const sizeBytes = finiteTelemetryNumber(directory && directory.sizeBytes, 0);
          const pct = Math.max(2, Math.min(100, (sizeBytes / maxBytes) * 100));
          const dirCard = document.createElement('div');
          dirCard.style.background = 'rgba(255,255,255,0.01)';
          dirCard.style.border = '1px solid var(--border)';
          dirCard.style.padding = '12px 16px';
          const header = document.createElement('div');
          header.style.display = 'flex';
          header.style.justifyContent = 'space-between';
          header.style.fontSize = '0.88rem';
          header.style.marginBottom = '6px';
          const path = document.createElement('code');
          path.textContent = String(directory && directory.path || '');
          const size = document.createElement('strong');
          size.style.color = 'var(--accent-warning)';
          size.textContent = String(directory && directory.sizeHuman || formatBytesJS(sizeBytes));
          header.appendChild(path);
          header.appendChild(size);
          const track = document.createElement('div');
          track.style.height = '6px';
          track.style.background = '#1f2937';
          track.style.overflow = 'hidden';
          const bar = document.createElement('div');
          bar.style.width = pct + '%';
          bar.style.height = '100%';
          bar.style.background = 'linear-gradient(90deg, #fbbf24, #d97706)';
          track.appendChild(bar);
          dirCard.appendChild(header);
          dirCard.appendChild(track);
          container.appendChild(dirCard);
        });
      }
    });

    // System telemetry auto-refresh: re-fetch CPU/mem/disk once per second and
    // update the gauges directly, without the manual button's loading-text flicker.
    let telemetryRefreshInFlight = false;
    async function refreshSystemTelemetry() {
      if (telemetryRefreshInFlight) return;
      telemetryRefreshInFlight = true;
      try {
        const resp = await fetch('/api/system', { headers: authHeaders() });
        if (!resp.ok) return;
        updateSystemTelemetry(await resp.json());
      } catch (_) {
      } finally {
        telemetryRefreshInFlight = false;
      }
    }

    let telemetryRefreshInterval = null;
    function startTelemetryAutoRefresh() {
      if (!telemetryRefreshInterval) telemetryRefreshInterval = setInterval(refreshSystemTelemetry, 1000);
    }
    function stopTelemetryAutoRefresh() {
      if (telemetryRefreshInterval) {
        clearInterval(telemetryRefreshInterval);
        telemetryRefreshInterval = null;
      }
    }

    const telemetryToggleBtn = document.getElementById('toggle-telemetry-refresh');
    if (telemetryToggleBtn) {
      telemetryToggleBtn.addEventListener('click', function() {
        if (telemetryRefreshInterval) {
          stopTelemetryAutoRefresh();
          this.textContent = veilT('telemetry.autoRefreshOff');
          this.classList.remove('danger');
          this.classList.add('secondary');
        } else {
          refreshSystemTelemetry();
          startTelemetryAutoRefresh();
          this.textContent = veilT('telemetry.autoRefreshOn');
          this.classList.remove('secondary');
          this.classList.add('danger');
        }
      });
    }
    window.addEventListener('beforeunload', stopTelemetryAutoRefresh);
    window.addEventListener('DOMContentLoaded', () => {
      if (telemetryToggleBtn) telemetryToggleBtn.textContent = veilT('telemetry.autoRefreshOn');
      refreshSystemTelemetry();
      startTelemetryAutoRefresh();
    });`
}
