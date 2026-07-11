package panel

// panelBackupsReliabilityJS keeps the restore mutation lock held while the
// server-side restore job is still running. Temporary restart/network/5xx or
// malformed-response failures are retried instead of enabling conflicting
// backup operations while the restore continues in the background.
func panelBackupsReliabilityJS() string {
	return `
    const baseSetBackupControlsDisabled = setBackupControlsDisabled;
    setBackupControlsDisabled = function(disabled) {
      baseSetBackupControlsDisabled(disabled);
      const refreshButton = document.getElementById('btn-load-backups');
      if (refreshButton) refreshButton.disabled = Boolean(disabled) || isViewerRole();
    };

    pollBackupRestore = async function(id, generation) {
      let lastError = '';
      for (let attempt = 0; attempt < 120; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 1000));
        if (generation !== backupRestorePollGeneration) return null;
        try {
          const response = await fetch('/api/backup-restore-jobs/' + encodeURIComponent(id), { headers: authHeaders() });
          const text = await response.text();
          if (generation !== backupRestorePollGeneration) return null;
          if (!response.ok) {
            const message = formatAPIError(text, response.status);
            if (response.status === 401 || response.status === 403 || response.status === 404) {
              setBackupOutput(message);
              return null;
            }
            lastError = message;
            setBackupOutput('Restore status check failed; retrying (' + (attempt + 1) + '/120): ' + message);
            continue;
          }
          const job = text ? JSON.parse(text) : null;
          if (!job || typeof job !== 'object' || Array.isArray(job) || !job.status) {
            throw new Error('Invalid backup restore status response.');
          }
          setBackupOutput(job);
          if (job.status === 'succeeded') {
            clearStoredPanelIdentity();
            window.location.reload();
            return job;
          }
          if (job.status === 'failed') return job;
        } catch (error) {
          if (generation !== backupRestorePollGeneration) return null;
          lastError = String(error && error.message ? error.message : error);
          setBackupOutput('Restore status check failed; retrying (' + (attempt + 1) + '/120): ' + lastError);
        }
      }
      const timeoutMessage = veilT('status.restoreTimedOut');
      setBackupOutput(lastError ? timeoutMessage + '\n\nLast polling error: ' + lastError : timeoutMessage);
      return null;
    };
`
}
