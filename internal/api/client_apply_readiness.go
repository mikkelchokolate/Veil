package api

import (
	"encoding/json"
	"sort"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/model"
)

type clientApplyFlags struct {
	failed  bool
	pending bool
}

type clientApplyReadinessCache struct {
	desired, applied uint64
	jobID, jobStatus string
	flags            map[string]clientApplyFlags
}

func (s *managementState) clientApplyReadiness(clientID string) (bool, bool) {
	if s == nil || s.applyRevisions == nil || s.applySnapshots == nil {
		return false, false
	}
	rev, err := s.applyRevisions.Get()
	if err != nil {
		return false, true
	}
	if rev.Desired == 0 || rev.Desired <= rev.Applied {
		return false, false
	}
	var job apply.Job
	hasJob := false
	if s.applyJobs != nil {
		jobs, jobErr := s.applyJobs.List(1)
		if jobErr != nil {
			return false, true
		}
		if len(jobs) > 0 {
			job = jobs[0]
			hasJob = true
		}
	}
	jobID, jobStatus := "", ""
	if hasJob {
		jobID, jobStatus = job.ID, job.Status
	}
	s.applyReadinessMu.Lock()
	defer s.applyReadinessMu.Unlock()
	if s.applyReadinessCache.desired == rev.Desired && s.applyReadinessCache.applied == rev.Applied &&
		s.applyReadinessCache.jobID == jobID && s.applyReadinessCache.jobStatus == jobStatus && s.applyReadinessCache.flags != nil {
		f := s.applyReadinessCache.flags[clientID]
		return f.failed, f.pending
	}
	flags := s.computeClientApplyFlags(rev, job, hasJob)
	s.applyReadinessCache = clientApplyReadinessCache{
		desired: rev.Desired, applied: rev.Applied, jobID: jobID, jobStatus: jobStatus, flags: flags,
	}
	f := flags[clientID]
	return f.failed, f.pending
}

func (s *managementState) computeClientApplyFlags(rev apply.Revisions, job apply.Job, hasJob bool) map[string]clientApplyFlags {
	flags := map[string]clientApplyFlags{}
	failed, pending := clientApplyJobFlags(job, hasJob, rev)
	desired, err := s.loadRevisionSnapshotLocked(rev.Desired)
	if err != nil {
		return flags
	}
	var applied model.ManagementSnapshot
	if rev.Applied > 0 {
		if loaded, loadErr := s.loadRevisionSnapshotLocked(rev.Applied); loadErr == nil {
			applied = loaded
		}
	}
	appliedFP := clientRuntimeFingerprints(applied)
	for clientID, fp := range clientRuntimeFingerprints(desired) {
		if appliedFP[clientID] == fp && fp != "" {
			continue
		}
		flags[clientID] = clientApplyFlags{failed: failed, pending: pending && !failed}
	}
	return flags
}

func clientApplyJobFlags(job apply.Job, hasJob bool, rev apply.Revisions) (failed, pending bool) {
	if rev.Desired <= rev.Applied {
		return false, false
	}
	if !hasJob || job.DesiredRevision != rev.Desired {
		return false, true
	}
	switch job.Status {
	case apply.StatusFailed, apply.StatusRolledBack, apply.StatusRollbackFailed, apply.StatusRecoveryPending:
		return true, false
	default:
		return false, true
	}
}

type clientRuntimeBinding struct {
	ID, InboundID, RuntimeIdentity string
	Enabled                        bool
}

type clientRuntimeCred struct {
	BindingID string
	Kind      string
	Version   int
	Material  string
}

type clientRuntimeMaterial struct {
	Enabled          bool
	Name             string
	Depleted         bool
	ExpiresAt        int64
	HasExpiry        bool
	QuotaBytes       int64
	HasQuota         bool
	QuotaResetPolicy string
	Bindings         []clientRuntimeBinding
	Creds            []clientRuntimeCred
}

func clientRuntimeFingerprints(snapshot model.ManagementSnapshot) map[string]string {
	// Group once so each client's fingerprint only scans its own rows.
	clients := make(map[string]*model.ManagementSnapshot, len(snapshot.Clients))
	for _, current := range snapshot.Clients {
		if clients[current.ID] == nil {
			clients[current.ID] = &model.ManagementSnapshot{Clients: []model.ClientSnapshot{current}}
		}
	}
	owners := make(map[string]string, len(snapshot.Bindings))
	for _, binding := range snapshot.Bindings {
		if current := clients[binding.ClientID]; current != nil {
			current.Bindings = append(current.Bindings, binding)
			owners[binding.ID] = binding.ClientID
		}
	}
	for _, credential := range snapshot.Credentials {
		if owner, ok := owners[credential.BindingID]; ok {
			clients[owner].Credentials = append(clients[owner].Credentials, credential)
		}
	}
	out := make(map[string]string, len(snapshot.Clients))
	for id, current := range clients {
		out[id] = clientRuntimeFingerprint(*current, id)
	}
	return out
}

func clientRuntimeFingerprint(snapshot model.ManagementSnapshot, clientID string) string {
	var material clientRuntimeMaterial
	found := false
	for _, current := range snapshot.Clients {
		if current.ID != clientID {
			continue
		}
		found = true
		material.Enabled = current.Enabled
		material.Name = current.Name
		material.Depleted = current.Depleted
		material.QuotaResetPolicy = current.QuotaResetPolicy
		if current.ExpiresAt != nil {
			material.HasExpiry = true
			material.ExpiresAt = *current.ExpiresAt
		}
		if current.QuotaBytes != nil {
			material.HasQuota = true
			material.QuotaBytes = *current.QuotaBytes
		}
		break
	}
	if !found {
		return ""
	}
	bindingIDs := map[string]struct{}{}
	for _, binding := range snapshot.Bindings {
		if binding.ClientID != clientID {
			continue
		}
		material.Bindings = append(material.Bindings, clientRuntimeBinding{
			ID: binding.ID, InboundID: binding.InboundID, RuntimeIdentity: binding.RuntimeIdentity, Enabled: binding.Enabled,
		})
		bindingIDs[binding.ID] = struct{}{}
	}
	sort.Slice(material.Bindings, func(i, j int) bool { return material.Bindings[i].ID < material.Bindings[j].ID })
	for _, cred := range snapshot.Credentials {
		if _, ok := bindingIDs[cred.BindingID]; !ok {
			continue
		}
		material.Creds = append(material.Creds, clientRuntimeCred{
			BindingID: cred.BindingID, Kind: cred.Kind, Version: cred.CredentialVersion, Material: string(cred.EncryptedValue),
		})
	}
	sort.Slice(material.Creds, func(i, j int) bool {
		if material.Creds[i].BindingID != material.Creds[j].BindingID {
			return material.Creds[i].BindingID < material.Creds[j].BindingID
		}
		return material.Creds[i].Version < material.Creds[j].Version
	})
	raw, err := json.Marshal(material)
	if err != nil {
		return ""
	}
	return string(raw)
}
