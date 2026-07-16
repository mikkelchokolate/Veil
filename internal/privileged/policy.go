package privileged

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/protocols"
)

type ErrorCode string

const (
	ErrorInvalidRequest     ErrorCode = "invalid_request"
	ErrorForbiddenOperation ErrorCode = "forbidden_operation"
	ErrorNotFound           ErrorCode = "not_found"
	ErrorConflict           ErrorCode = "conflict"
	ErrorOperationFailed    ErrorCode = "operation_failed"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ArtifactPath struct {
	Staged    string
	Generated string
}

type Policy struct {
	StagingRoot          string
	GeneratedRoot        string
	StateRoot            string
	StatePath            string
	KeyPath              string
	BackupPassphrasePath string
	BackupRoot           string
	UpdateRoot           string
	ManagedUnits         map[string]struct{}
	ManagedUnitPrefixes  []string
	Artifacts            map[string]ArtifactPath
	UpdateArtifacts      map[string]string
	FirewallRules        map[string]struct{}
	// AllowedArtifactNames restricts dynamically promoted artifact names to a
	// known set. Static per-protocol artifacts and update artifacts are not
	// constrained by this set. A nil or empty map means no extra restriction.
	AllowedArtifactNames map[string]struct{}
}

type ResolvedArtifact struct {
	ID          string
	Source      string
	Destination string
}

type ResolvedPromotion struct {
	Artifacts       []ResolvedArtifact
	RemoveArtifacts []ResolvedArtifact
	RestoreBackupID string
}

type ResolvedJournal struct {
	Unit  string
	Lines int
}

type ResolvedBackup struct {
	Action               BackupAction
	ArchiveName          string
	ArchivePath          string
	BackupRoot           string
	StateRoot            string
	StatePath            string
	KeyPath              string
	BackupPassphrasePath string
	Daily                int
	Weekly               int
	Monthly              int
	CheckOnly            bool
	AllowVersionMismatch bool
}

type ResolvedFirewall struct {
	RuleIDs []string
	Rules   []FirewallRule
}

type ResolvedUpdate struct {
	ArtifactID    string
	Version       string
	Path          string
	ChecksumsPath string
}

func (p Policy) ValidateServiceAction(request ServiceActionRequest) error {
	if !p.allowsUnit(request.Unit) {
		return newError(ErrorForbiddenOperation, "service unit is not managed")
	}
	switch request.Action {
	case ServiceActionStart, ServiceActionStop, ServiceActionRestart, ServiceActionReload, ServiceActionEnable, ServiceActionDisable:
		return nil
	default:
		return newError(ErrorInvalidRequest, "unsupported service action")
	}
}

func (p Policy) ValidateServiceStatus(request ServiceStatusRequest) error {
	if len(request.Units) == 0 {
		return newError(ErrorInvalidRequest, "at least one service unit is required")
	}
	for _, unit := range request.Units {
		if !p.allowsUnit(unit) {
			return newError(ErrorForbiddenOperation, "service unit is not managed")
		}
	}
	return nil
}

func (p Policy) ResolveJournal(request JournalRequest) (ResolvedJournal, error) {
	if !p.allowsUnit(request.Unit) {
		return ResolvedJournal{}, newError(ErrorForbiddenOperation, "journal unit is not managed")
	}
	lines := request.Lines
	if lines < 1 {
		lines = 1
	}
	if lines > 1000 {
		lines = 1000
	}
	return ResolvedJournal{Unit: request.Unit, Lines: lines}, nil
}

func (p Policy) ResolvePromotion(request PromoteRequest) (ResolvedPromotion, error) {
	if request.RestoreBackupID != "" {
		if len(request.ArtifactIDs) != 0 || len(request.RemoveArtifactIDs) != 0 ||
			!opaquePromotionIDPattern.MatchString(request.RestoreBackupID) {
			return ResolvedPromotion{}, newError(ErrorInvalidRequest, "invalid promotion restore request")
		}
		return ResolvedPromotion{RestoreBackupID: request.RestoreBackupID}, nil
	}
	artifacts, err := p.resolveArtifacts(request.ArtifactIDs, false)
	if err != nil {
		return ResolvedPromotion{}, err
	}
	removeArtifacts, err := p.resolveArtifacts(request.RemoveArtifactIDs, true)
	if err != nil {
		return ResolvedPromotion{}, err
	}
	if len(artifacts) == 0 && len(removeArtifacts) == 0 {
		return ResolvedPromotion{}, newError(ErrorInvalidRequest, "at least one artifact is required")
	}
	return ResolvedPromotion{Artifacts: artifacts, RemoveArtifacts: removeArtifacts}, nil
}

func (p Policy) resolveArtifacts(ids []string, allowLegacyCaddyRemoval bool) ([]ResolvedArtifact, error) {
	resolved := make([]ResolvedArtifact, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			return nil, newError(ErrorConflict, "duplicate artifact id")
		}
		seen[id] = struct{}{}
		spec, ok := p.Artifacts[id]
		if !ok {
			spec, ok = p.managedArtifactPath(id)
		}
		if !ok && allowLegacyCaddyRemoval {
			spec, ok = legacyCaddyArtifactPath(id)
		}
		if !ok {
			return nil, newError(ErrorNotFound, "unknown artifact id")
		}
		source, err := resolveBelow(p.StagingRoot, spec.Staged)
		if err != nil {
			return nil, err
		}
		destination, err := resolveBelow(p.GeneratedRoot, spec.Generated)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, ResolvedArtifact{ID: id, Source: source, Destination: destination})
	}
	return resolved, nil
}

func legacyCaddyArtifactPath(id string) (ArtifactPath, bool) {
	clean := filepath.ToSlash(filepath.Clean(id))
	if clean != id || strings.Contains(clean, `\`) || !strings.HasPrefix(clean, "caddy/") {
		return ArtifactPath{}, false
	}
	rest := strings.TrimPrefix(clean, "caddy/")
	if strings.Contains(rest, "/") || !strings.HasSuffix(rest, ".Caddyfile") {
		return ArtifactPath{}, false
	}
	name := strings.TrimSuffix(rest, ".Caddyfile")
	if !artifactNamePattern.MatchString(name) {
		return ArtifactPath{}, false
	}
	path := filepath.FromSlash(clean)
	return ArtifactPath{Staged: path, Generated: path}, true
}

var (
	opaquePromotionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	artifactNamePattern      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	updateVersionPattern     = regexp.MustCompile(`^v?[0-9][A-Za-z0-9._+-]*$`)
)

func (p Policy) managedArtifactPath(id string) (ArtifactPath, bool) {
	clean := filepath.ToSlash(filepath.Clean(id))
	if clean != id || strings.Contains(clean, `\`) {
		return ArtifactPath{}, false
	}
	if clean == "sing-box/warp.json" {
		return ArtifactPath{Staged: filepath.FromSlash(clean), Generated: filepath.FromSlash(clean)}, true
	}
	return managedProtocolArtifactID(clean, p.AllowedArtifactNames)
}

func managedProtocolArtifactID(clean string, allowedNames map[string]struct{}) (ArtifactPath, bool) {
	registry := protocols.NewRegistry()
	for _, plugin := range registry.All() {
		cr, ok := protocols.AsConfigRenderer(plugin)
		if !ok {
			continue
		}
		sub := filepath.ToSlash(cr.ArtifactSpec().Subpath)
		if sub == "" {
			continue
		}
		if clean == sub {
			return ArtifactPath{Staged: filepath.FromSlash(clean), Generated: filepath.FromSlash(clean)}, true
		}
		if ok, name := protocolAllowsDynamicArtifact(plugin, sub, clean, allowedNames); ok {
			_ = name
			return ArtifactPath{Staged: filepath.FromSlash(clean), Generated: filepath.FromSlash(clean)}, true
		}
	}
	return ArtifactPath{}, false
}

func protocolAllowsDynamicArtifact(plugin protocols.ProtocolPlugin, sub string, clean string, allowedNames map[string]struct{}) (bool, string) {
	if !protocolHasTemplateRuntime(plugin) {
		return false, ""
	}
	dir := filepath.ToSlash(filepath.Dir(sub))
	if dir == "." || !strings.HasPrefix(clean, dir+"/") {
		return false, ""
	}
	rest := strings.TrimPrefix(clean, dir+"/")
	if strings.Contains(rest, "/") {
		return false, ""
	}
	ext := filepath.Ext(filepath.Base(sub))
	if ext == "" || !strings.HasSuffix(rest, ext) {
		return false, ""
	}
	name := strings.TrimSuffix(rest, ext)
	if !artifactNamePattern.MatchString(name) {
		return false, ""
	}
	if len(allowedNames) > 0 {
		if _, ok := allowedNames[name]; !ok {
			return false, ""
		}
	}
	return true, name
}

func protocolHasTemplateRuntime(plugin protocols.ProtocolPlugin) bool {
	rp, ok := protocols.AsRuntimeProvider(plugin)
	if !ok {
		return false
	}
	for _, descriptor := range rp.RuntimeDescriptors(nil) {
		if descriptor.TemplateUnit != "" || strings.Contains(descriptor.Unit, "@") {
			return true
		}
	}
	return false
}

func (p Policy) ResolveBackup(request BackupRequest) (ResolvedBackup, error) {
	switch request.Action {
	case BackupActionCreate, BackupActionList, BackupActionVerify, BackupActionRead, BackupActionPrune, BackupActionRestore:
	default:
		return ResolvedBackup{}, newError(ErrorInvalidRequest, "unsupported backup action")
	}
	resolved := ResolvedBackup{
		Action:               request.Action,
		BackupRoot:           p.BackupRoot,
		StateRoot:            p.StateRoot,
		StatePath:            p.StatePath,
		KeyPath:              p.KeyPath,
		BackupPassphrasePath: p.BackupPassphrasePath,
		Daily:                request.Daily,
		Weekly:               request.Weekly,
		Monthly:              request.Monthly,
		CheckOnly:            request.CheckOnly,
		AllowVersionMismatch: request.AllowVersionMismatch,
	}
	if resolved.StatePath == "" {
		resolved.StatePath = filepath.Join(p.StateRoot, "state.json")
	}
	if resolved.KeyPath == "" {
		resolved.KeyPath = filepath.Join(p.StateRoot, "state.key")
	}
	if resolved.BackupPassphrasePath == "" {
		resolved.BackupPassphrasePath = filepath.Join(p.StateRoot, "backup.passphrase")
	}
	requiresArchive := request.Action == BackupActionVerify || request.Action == BackupActionRead || request.Action == BackupActionRestore
	if request.ArchiveName == "" {
		if requiresArchive {
			return ResolvedBackup{}, newError(ErrorInvalidRequest, "archiveName is required")
		}
		return resolved, nil
	}
	if strings.ContainsAny(request.ArchiveName, `/\`) ||
		filepath.Base(request.ArchiveName) != request.ArchiveName ||
		!strings.HasSuffix(strings.ToLower(request.ArchiveName), ".enc") {
		return ResolvedBackup{}, newError(ErrorInvalidRequest, "archiveName must be an .enc basename")
	}
	archivePath, err := resolveBelow(p.BackupRoot, request.ArchiveName)
	if err != nil {
		return ResolvedBackup{}, err
	}
	resolved.ArchiveName = request.ArchiveName
	resolved.ArchivePath = archivePath
	return resolved, nil
}

var ufwAllowRulePattern = regexp.MustCompile(`^([1-9]\d{0,3}|[1-5]\d{4}|6[0-4]\d{3}|65[0-4]\d{2}|655[0-2]\d|6553[0-5])/(tcp|udp)$`)

func (p Policy) ResolveFirewall(request FirewallRequest) (ResolvedFirewall, error) {
	if len(request.Rules) > 0 {
		for _, rule := range request.Rules {
			if err := validateUFWRule(rule); err != nil {
				return ResolvedFirewall{}, newError(ErrorInvalidRequest, err.Error())
			}
		}
		return ResolvedFirewall{Rules: request.Rules}, nil
	}
	if len(request.RuleIDs) == 0 {
		return ResolvedFirewall{}, newError(ErrorInvalidRequest, "at least one firewall rule is required")
	}
	rules := make([]string, 0, len(request.RuleIDs))
	for _, id := range request.RuleIDs {
		if _, ok := p.FirewallRules[id]; !ok {
			return ResolvedFirewall{}, newError(ErrorForbiddenOperation, "firewall rule is not managed")
		}
		rules = append(rules, id)
	}
	return ResolvedFirewall{RuleIDs: rules}, nil
}

func isUFWCommentClause(args []string, idx int) bool {
	if idx >= len(args) {
		return false
	}
	if args[idx] != "comment" {
		return false
	}
	if idx != len(args)-2 {
		return false
	}
	if len(args[idx+1]) == 0 {
		return false
	}
	return true
}

func validateUFWRule(rule FirewallRule) error {
	if rule.Command != "ufw" {
		return fmt.Errorf("unsupported firewall command %q", rule.Command)
	}
	if len(rule.Args) < 2 {
		return fmt.Errorf("ufw rule is too short")
	}
	if rule.Args[0] != "allow" {
		return fmt.Errorf("unsupported ufw action %q", rule.Args[0])
	}
	if !ufwAllowRulePattern.MatchString(rule.Args[1]) {
		return fmt.Errorf("invalid ufw allow target %q", rule.Args[1])
	}
	if len(rule.Args) > 2 {
		arg := rule.Args[2]
		if strings.ContainsAny(arg, ";|&$`\"'\\") {
			return fmt.Errorf("disallowed character in firewall rule argument")
		}
		if arg != "comment" {
			return fmt.Errorf("unsupported firewall argument %q", arg)
		}
		if !isUFWCommentClause(rule.Args[2:], 0) {
			return fmt.Errorf("comment must be the final clause")
		}
	}
	return nil
}

func (p Policy) ResolveUpdate(request UpdateRequest) (ResolvedUpdate, error) {
	relative, ok := p.UpdateArtifacts[request.ArtifactID]
	if !ok {
		return ResolvedUpdate{}, newError(ErrorNotFound, "unknown update artifact id")
	}
	if request.Version == "" || len(request.Version) > 64 || !updateVersionPattern.MatchString(request.Version) {
		return ResolvedUpdate{}, newError(ErrorInvalidRequest, "invalid update version")
	}
	path, err := resolveBelow(p.UpdateRoot, relative)
	if err != nil {
		return ResolvedUpdate{}, err
	}
	checksumsPath, err := resolveBelow(p.UpdateRoot, "checksums.txt")
	if err != nil {
		return ResolvedUpdate{}, err
	}
	return ResolvedUpdate{
		ArtifactID: request.ArtifactID, Version: request.Version, Path: path, ChecksumsPath: checksumsPath,
	}, nil
}

func (p Policy) allowsUnit(unit string) bool {
	if _, ok := p.ManagedUnits[unit]; ok {
		return true
	}
	for _, prefix := range p.ManagedUnitPrefixes {
		if strings.HasPrefix(unit, prefix) &&
			strings.HasSuffix(unit, ".service") &&
			!strings.ContainsAny(unit, `/\; "'`) {
			return true
		}
	}
	return false
}

func resolveBelow(root, relative string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(relative) == "" {
		return "", newError(ErrorInvalidRequest, "managed root and relative path are required")
	}
	if filepath.IsAbs(relative) {
		return "", newError(ErrorForbiddenOperation, "absolute managed paths are forbidden")
	}
	cleanRelative := filepath.Clean(relative)
	if cleanRelative == "." || cleanRelative == ".." || strings.HasPrefix(cleanRelative, ".."+string(filepath.Separator)) {
		return "", newError(ErrorForbiddenOperation, "managed path escapes its root")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", newError(ErrorOperationFailed, "resolve managed root")
	}
	candidate := filepath.Join(absoluteRoot, cleanRelative)
	if !pathWithin(absoluteRoot, candidate) {
		return "", newError(ErrorForbiddenOperation, "managed path escapes its root")
	}

	resolvedRoot, err := resolveExistingPrefix(absoluteRoot)
	if err != nil {
		return "", newError(ErrorOperationFailed, "resolve managed root symlinks")
	}
	resolvedCandidate, err := resolveExistingPrefix(candidate)
	if err != nil {
		return "", newError(ErrorOperationFailed, "resolve managed path symlinks")
	}
	if !pathWithin(resolvedRoot, resolvedCandidate) {
		return "", newError(ErrorForbiddenOperation, "managed path follows a symlink outside its root")
	}
	return candidate, nil
}

func resolveExistingPrefix(path string) (string, error) {
	missing := []string{}
	current := filepath.Clean(path)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func newError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

func wrapOperationError(err error) error {
	if err == nil {
		return nil
	}
	var operationError *Error
	if errors.As(err, &operationError) {
		return operationError
	}
	return newError(ErrorOperationFailed, fmt.Sprintf("privileged operation failed: %v", err))
}
