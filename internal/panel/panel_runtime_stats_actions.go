package panel

const panelRuntimeStatsActionsPlaceholder = "__VEIL_PANEL_RUNTIME_STATS_ACTIONS__"

func panelRuntimeStatsActionsJS() string {
	return `    // Helper formats
    function formatBytesJS(bytes) {
      if (bytes === 0) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(k));
      return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    function formatDurationJS(seconds) {
      if (!seconds) return '0s';
      const d = Math.floor(seconds / (3600*24));
      const h = Math.floor((seconds % (3600*24)) / 3600);
      const m = Math.floor((seconds % 3600) / 60);
      const s = Math.floor(seconds % 60);
      
      const parts = [];
      if (d > 0) parts.push(d + 'd');
      if (h > 0) parts.push(h + 'h');
      if (m > 0) parts.push(m + 'm');
      if (s > 0 || parts.length === 0) parts.push(s + 's');
      return parts.join(' ');
    }

    window.toggleRawView = function(id) {
      const pre = document.getElementById(id);
      if (pre) {
        pre.style.display = pre.style.display === 'none' ? 'block' : 'none';
      }
    };

    function updateSystemTelemetry(stats) {
      const circ = 314; // 2 * pi * r (r=50)

      // CPU
      const cpuVal = stats.cpuPercent || 0;
      const cpuOffset = circ - (Math.min(100, Math.max(0, cpuVal)) / 100) * circ;
      document.getElementById('cpu-circle').style.strokeDashoffset = cpuOffset;
      document.getElementById('cpu-text').innerText = cpuVal.toFixed(1) + '%';
      
      const loadAvg1 = stats.loadAvg1 || 0;
      const loadAvg5 = stats.loadAvg5 || 0;
      const loadAvg15 = stats.loadAvg15 || 0;
      document.getElementById('cpu-load-details').innerText = '1m/5m/15m: ' + loadAvg1.toFixed(2) + ' / ' + loadAvg5.toFixed(2) + ' / ' + loadAvg15.toFixed(2);

      // Memory
      const memTotal = stats.memoryTotalMB || 1;
      const memUsed = stats.memoryUsedMB || 0;
      const memPercent = (memUsed / memTotal) * 100;
      const memOffset = circ - (Math.min(100, Math.max(0, memPercent)) / 100) * circ;
      document.getElementById('mem-circle').style.strokeDashoffset = memOffset;
      document.getElementById('mem-text').innerText = memPercent.toFixed(1) + '%';
      document.getElementById('mem-usage-details').innerText = memUsed + ' MB / ' + memTotal + ' MB';

      // Disk
      const diskTotal = stats.diskTotalGB || 1;
      const diskUsed = stats.diskUsedGB || 0;
      const diskPercent = (diskUsed / diskTotal) * 100;
      const diskOffset = circ - (Math.min(100, Math.max(0, diskPercent)) / 100) * circ;
      document.getElementById('disk-circle').style.strokeDashoffset = diskOffset;
      document.getElementById('disk-text').innerText = diskPercent.toFixed(1) + '%';
      document.getElementById('disk-usage-details').innerText = diskUsed.toFixed(1) + ' GB / ' + diskTotal.toFixed(1) + ' GB';
    }

    document.getElementById('load-system-stats').addEventListener('click', async () => {
      const stats = await loadJSON('/api/system', 'system-stats-output');
      if (stats) {
        updateSystemTelemetry(stats);
      }
    });

    document.getElementById('load-network-stats').addEventListener('click', async () => {
      const net = await loadJSON('/api/network', 'network-stats-output');
      if (net && net.interfaces) {
        const tbody = document.getElementById('net-interfaces-tbody');
        tbody.innerHTML = '';
        net.interfaces.forEach(iface => {
          const row = document.createElement('tr');
          row.innerHTML = '<td><strong style="color: #fff; font-size: 0.95rem;">' + iface.name + '</strong></td>' +
            '<td><span style="color: #34d399; font-weight: 500;">&darr; ' + formatBytesJS(iface.rxBytes) + '</span></td>' +
            '<td><span style="color: #60a5fa; font-weight: 500;">&uarr; ' + formatBytesJS(iface.txBytes) + '</span></td>' +
            '<td style="color: var(--text-muted);">' + iface.rxPackets.toLocaleString() + ' / ' + iface.txPackets.toLocaleString() + '</td>';
          tbody.appendChild(row);
        });
        document.getElementById('net-interfaces-table').style.display = 'block';
      }
    });

    document.getElementById('load-connections-stats').addEventListener('click', async () => {
      const conn = await loadJSON('/api/connections', 'connections-stats-output');
      if (conn && conn.listeners) {
        const container = document.getElementById('connections-list');
        container.innerHTML = '';
        if (conn.listeners.length === 0) {
          container.innerHTML = '<span style="color: var(--text-muted);">No listening ports found.</span>';
          return;
        }
        conn.listeners.forEach(lis => {
          const badge = document.createElement('span');
          badge.className = 'badge';
          badge.style.background = lis.proto === 'tcp' ? 'rgba(79, 70, 229, 0.15)' : 'rgba(245, 158, 11, 0.15)';
          badge.style.color = lis.proto === 'tcp' ? '#818cf8' : '#fbbf24';
          badge.style.border = '1px solid ' + (lis.proto === 'tcp' ? 'rgba(79, 70, 229, 0.3)' : 'rgba(245, 158, 11, 0.3)');
          badge.style.padding = '6px 12px';
          badge.style.borderRadius = '8px';
          badge.style.fontSize = '0.85rem';
          
          const procText = lis.process ? ' <span style="opacity: 0.7; font-size: 0.75rem; background: rgba(255,255,255,0.08); padding: 2px 6px; border-radius: 4px; margin-left: 6px;">' + lis.process + '</span>' : '';
          badge.innerHTML = '<strong style="text-transform: uppercase;">' + lis.proto + '</strong> &nbsp;:' + lis.port + procText;
          container.appendChild(badge);
        });
      }
    });

    document.getElementById('load-processes-stats').addEventListener('click', async () => {
      const proc = await loadJSON('/api/processes', 'processes-stats-output');
      if (proc && proc.processes) {
        const tbody = document.getElementById('processes-tbody');
        tbody.innerHTML = '';
        if (proc.processes.length === 0) {
          tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--text-muted);">No managed processes running.</td></tr>';
          return;
        }
        proc.processes.forEach(p => {
          const row = document.createElement('tr');
          row.innerHTML = '<td style="font-family: monospace; color: var(--text-muted);">' + p.pid + '</td>' +
            '<td><strong style="color: #fff;">' + p.name + '</strong></td>' +
            '<td><span class="badge badge-success" style="background: rgba(16, 185, 129, 0.1); color: #34d399;">' + p.cpuPercent.toFixed(1) + '%</span></td>' +
            '<td>' + p.memoryMB + ' MB</td>' +
            '<td>' + formatDurationJS(p.uptimeSeconds) + '</td>';
          tbody.appendChild(row);
        });
        document.getElementById('processes-table').style.display = 'block';
      }
    });

    document.getElementById('load-disk-stats').addEventListener('click', async () => {
      const disk = await loadJSON('/api/disk', 'disk-stats-output');
      if (disk && disk.dirs) {
        const container = document.getElementById('disk-paths-container');
        container.innerHTML = '';
        
        // Find max size to scale progress bars
        let maxBytes = 1;
        disk.dirs.forEach(d => {
          if (d.sizeBytes > maxBytes) maxBytes = d.sizeBytes;
        });

        disk.dirs.forEach(d => {
          const pct = Math.max(2, (d.sizeBytes / maxBytes) * 100);
          const dirCard = document.createElement('div');
          dirCard.style.background = 'rgba(255,255,255,0.01)';
          dirCard.style.border = '1px solid var(--border-color)';
          dirCard.style.borderRadius = '10px';
          dirCard.style.padding = '12px 16px';
          
          dirCard.innerHTML = '<div style="display: flex; justify-content: space-between; font-size: 0.88rem; margin-bottom: 6px;">' +
              '<code style="color: #fff; font-size: 0.9rem;">' + d.path + '</code>' +
              '<strong style="color: var(--accent-warning);">' + d.sizeHuman + '</strong>' +
            '</div>' +
            '<div style="height: 6px; background: #1f2937; border-radius: 3px; overflow: hidden;">' +
              '<div style="width: ' + pct + '%; height: 100%; background: linear-gradient(90deg, #fbbf24, #d97706); border-radius: 3px;"></div>' +
            '</div>';
          container.appendChild(dirCard);
        });
      }
    });

    // System telemetry auto-refresh: re-fetch CPU/mem/disk once per second and
    // update the gauges directly, without the manual button's loading-text
    // flicker. Runs by default so the dashboard is always live.
    async function refreshSystemTelemetry() {
      try {
        const resp = await fetch('/api/system', { headers: authHeaders() });
        if (!resp.ok) {
          return;
        }
        const stats = await resp.json();
        updateSystemTelemetry(stats);
      } catch (_) {
      }
    }

    let telemetryRefreshInterval = null;
    function startTelemetryAutoRefresh() {
      if (telemetryRefreshInterval) {
        return;
      }
      telemetryRefreshInterval = setInterval(refreshSystemTelemetry, 1000);
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

    // Start telemetry auto-refresh automatically on page load (default ON).
    window.addEventListener('DOMContentLoaded', () => {
      if (telemetryToggleBtn) {
        telemetryToggleBtn.textContent = veilT('telemetry.autoRefreshOn');
      }
      refreshSystemTelemetry();
      startTelemetryAutoRefresh();
    });`
}
