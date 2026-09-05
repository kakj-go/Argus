// Package trustbundle owns canonical public CA material and its versioned
// distribution state. It deliberately has no access to CA private keys.
package trustbundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	StateStable       = "stable"
	StatePreparing    = "preparing"
	StateOverlapping  = "overlapping"
	StateRetiring     = "retiring"
	StateFailed       = "failed"
	DirectionForward  = "forward"
	DirectionRollback = "rollback"
	// MinimumOverlap exceeds the maximum 24-hour external client identity
	// lifetime by a safety margin. This guarantees that both forward rotation
	// and a scheduled rollback can drain certificates from the retiring CA.
	MinimumOverlap = 32 * time.Hour
)

type Material struct {
	PEM          []byte
	SHA256       string
	Fingerprints []string
	Certificates []*x509.Certificate
}

type Bundle struct {
	Epoch                 int64
	State                 string
	Direction             string
	Material              Material
	CurrentCAFingerprints []string
	NextCAFingerprints    []string
	StartedAt             time.Time
	RetireAt              time.Time
	LastError             string
}

type Service struct {
	Store        *postgres.Store
	MountedPath  string
	InitialEpoch int64
}

type Node struct {
	Kind         string
	ID           string
	EnterpriseID uuid.NullUUID
}

type Acknowledgement struct {
	Epoch        int64
	SHA256       string
	Fingerprints []string
}

func ProcessNodeID(component string) string {
	hostname, _ := os.Hostname()
	component = strings.TrimSpace(component)
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "unknown"
	}
	value := component + "/" + hostname
	if len(value) > 256 {
		return value[:256]
	}
	return value
}

// PrepareRotation creates the immutable, dual-trust Bundle version and copies
// every known node into the new epoch. Recently active nodes block cutover;
// older offline nodes remain visible but do not block it. The caller must
// publish Bundle.Material before allowing certificates to move to the next
// issuer.
func (service Service) PrepareRotation(ctx context.Context, nextPEM []byte, activeSince time.Time, activeControlPlaneIDs ...string) (Bundle, error) {
	if service.Store == nil {
		return Bundle{}, errors.New("Trust Bundle store is not configured")
	}
	next, err := Parse(nextPEM, time.Now().UTC())
	if err != nil {
		return Bundle{}, fmt.Errorf("validate next Trust Bundle: %w", err)
	}
	var prepared Bundle
	err = service.Store.InTx(ctx, func(queries *db.Queries) error {
		currentRecord, getErr := queries.GetCurrentTrustBundle(ctx)
		if getErr != nil {
			return getErr
		}
		current, parseErr := fromRecord(currentRecord)
		if parseErr != nil {
			return parseErr
		}
		if current.State != StateStable {
			return fmt.Errorf("Trust Bundle epoch %d is %s; finish or abort it before rotating again", current.Epoch, current.State)
		}
		if slices.Equal(current.Material.Fingerprints, next.Fingerprints) {
			return errors.New("next Trust Bundle is identical to the current Bundle")
		}
		combined, mergeErr := Merge(current.Material, next)
		if mergeErr != nil {
			return mergeErr
		}
		now := time.Now().UTC()
		record, createErr := queries.CreateTrustBundle(ctx, db.CreateTrustBundleParams{
			Epoch: current.Epoch + 1, State: StatePreparing, BundlePem: string(combined.PEM), BundleSha256: combined.SHA256,
			CurrentCaFingerprints: current.Material.Fingerprints, NextCaFingerprints: next.Fingerprints,
			StartedAt: pgtype.Timestamptz{Time: now, Valid: true}, LastError: "",
		})
		if createErr != nil {
			return createErr
		}
		if activeSince.IsZero() {
			activeSince = now.Add(-10 * time.Minute)
		}
		if _, seedErr := queries.SeedTrustBundleNodes(ctx, db.SeedTrustBundleNodesParams{
			TargetEpoch: record.Epoch, TargetBundleSha256: combined.SHA256, TargetCaFingerprints: combined.Fingerprints,
			SourceEpoch: current.Epoch, ActiveSince: pgtype.Timestamptz{Time: activeSince.UTC(), Valid: true},
			ActiveControlPlaneIds: activeControlPlaneIDs,
		}); seedErr != nil {
			return seedErr
		}
		prepared, createErr = fromRecord(record)
		return createErr
	})
	return prepared, err
}

// PromoteOverlap starts the retirement clock only after every seeded online
// node has acknowledged the dual-trust Bundle.
func (service Service) PromoteOverlap(ctx context.Context, epoch int64, overlap time.Duration) (Bundle, error) {
	if service.Store == nil || overlap < MinimumOverlap {
		return Bundle{}, fmt.Errorf("Trust Bundle overlap must be at least %s", MinimumOverlap)
	}
	var promoted Bundle
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		record, getErr := queries.GetTrustBundle(ctx, epoch)
		if getErr != nil {
			return getErr
		}
		bundle, parseErr := fromRecord(record)
		if parseErr != nil {
			return parseErr
		}
		if bundle.State != StatePreparing {
			return fmt.Errorf("Trust Bundle epoch %d is %s, not preparing", epoch, bundle.State)
		}
		pending, countErr := queries.CountUnacknowledgedTrustNodes(ctx, epoch)
		if countErr != nil {
			return countErr
		}
		if pending != 0 {
			return fmt.Errorf("Trust Bundle epoch %d still has %d unacknowledged online nodes", epoch, pending)
		}
		record, updateErr := queries.UpdateTrustBundleState(ctx, db.UpdateTrustBundleStateParams{
			Epoch: epoch, State: StateOverlapping, BundlePem: string(bundle.Material.PEM), BundleSha256: bundle.Material.SHA256,
			CurrentCaFingerprints: bundle.CurrentCAFingerprints, NextCaFingerprints: bundle.NextCAFingerprints,
			RetireAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(overlap), Valid: true}, LastError: "",
		})
		if updateErr != nil {
			return updateErr
		}
		promoted, updateErr = fromRecord(record)
		return updateErr
	})
	return promoted, err
}

// ReverseOverlap schedules a safe rollback without removing either CA
// immediately. The former CA becomes the retirement target and the same
// minimum overlap gives every short-lived external identity time to rotate
// back before the other CA is removed.
func (service Service) ReverseOverlap(ctx context.Context, epoch int64, overlap time.Duration) (Bundle, error) {
	if service.Store == nil || overlap < MinimumOverlap {
		return Bundle{}, fmt.Errorf("Trust Bundle rollback overlap must be at least %s", MinimumOverlap)
	}
	record, err := service.Store.Queries.GetTrustBundle(ctx, epoch)
	if err != nil {
		return Bundle{}, err
	}
	bundle, err := fromRecord(record)
	if err != nil {
		return Bundle{}, err
	}
	if bundle.State != StatePreparing && bundle.State != StateOverlapping {
		return Bundle{}, fmt.Errorf("Trust Bundle epoch %d is %s and cannot be reversed", epoch, bundle.State)
	}
	if len(bundle.CurrentCAFingerprints) == 0 || len(bundle.NextCAFingerprints) == 0 {
		return Bundle{}, errors.New("Trust Bundle rotation has no reversible CA pair")
	}
	if bundle.Direction == DirectionRollback {
		return bundle, nil
	}
	reversed, err := service.Store.Queries.ReverseTrustBundleOverlap(ctx, db.ReverseTrustBundleOverlapParams{
		Epoch: epoch, RetireAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(overlap), Valid: true},
	})
	if err != nil {
		return Bundle{}, err
	}
	return fromRecord(reversed)
}

func (service Service) Extend(ctx context.Context, epoch int64, extension time.Duration) (Bundle, error) {
	if service.Store == nil || extension <= 0 {
		return Bundle{}, errors.New("extension must be positive")
	}
	record, err := service.Store.Queries.GetTrustBundle(ctx, epoch)
	if err != nil {
		return Bundle{}, err
	}
	bundle, err := fromRecord(record)
	if err != nil {
		return Bundle{}, err
	}
	if bundle.State != StateOverlapping || bundle.RetireAt.IsZero() {
		return Bundle{}, fmt.Errorf("Trust Bundle epoch %d is not overlapping", epoch)
	}
	record, err = service.Store.Queries.UpdateTrustBundleState(ctx, db.UpdateTrustBundleStateParams{
		Epoch: epoch, State: StateOverlapping, BundlePem: string(bundle.Material.PEM), BundleSha256: bundle.Material.SHA256,
		CurrentCaFingerprints: bundle.CurrentCAFingerprints, NextCaFingerprints: bundle.NextCAFingerprints,
		RetireAt: pgtype.Timestamptz{Time: bundle.RetireAt.Add(extension), Valid: true}, LastError: "",
	})
	if err != nil {
		return Bundle{}, err
	}
	return fromRecord(record)
}

// CompleteRetirement makes the next CA set the only trusted material and
// advances the epoch. Nodes that missed the fixed overlap are deliberately
// marked trust_expired and must use a repair command.
func (service Service) CompleteRetirement(ctx context.Context, epoch int64, activeControlPlaneIDs ...string) (Bundle, error) {
	if service.Store == nil {
		return Bundle{}, errors.New("Trust Bundle store is not configured")
	}
	var stable Bundle
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		record, getErr := queries.GetTrustBundle(ctx, epoch)
		if getErr != nil {
			return getErr
		}
		bundle, parseErr := fromRecord(record)
		if parseErr != nil {
			return parseErr
		}
		if bundle.State != StateOverlapping || bundle.RetireAt.IsZero() || time.Now().UTC().Before(bundle.RetireAt) {
			return fmt.Errorf("Trust Bundle epoch %d has not reached its retirement deadline", epoch)
		}
		next, selectErr := bundle.Material.Select(bundle.NextCAFingerprints)
		if selectErr != nil {
			return selectErr
		}
		if _, updateErr := queries.UpdateTrustBundleState(ctx, db.UpdateTrustBundleStateParams{
			Epoch: epoch, State: StateRetiring, BundlePem: string(bundle.Material.PEM), BundleSha256: bundle.Material.SHA256,
			CurrentCaFingerprints: bundle.CurrentCAFingerprints, NextCaFingerprints: bundle.NextCAFingerprints,
			RetireAt: pgtype.Timestamptz{Time: bundle.RetireAt, Valid: true}, LastError: "",
		}); updateErr != nil {
			return updateErr
		}
		if _, expireErr := queries.MarkUnacknowledgedTrustExpired(ctx, epoch); expireErr != nil {
			return expireErr
		}
		now := time.Now().UTC()
		stableRecord, createErr := queries.CreateTrustBundle(ctx, db.CreateTrustBundleParams{
			Epoch: epoch + 1, State: StateStable, BundlePem: string(next.PEM), BundleSha256: next.SHA256,
			CurrentCaFingerprints: next.Fingerprints, NextCaFingerprints: []string{},
			StartedAt: pgtype.Timestamptz{Time: now, Valid: true}, LastError: "",
		})
		if createErr != nil {
			return createErr
		}
		if _, seedErr := queries.SeedTrustBundleNodes(ctx, db.SeedTrustBundleNodesParams{
			TargetEpoch: stableRecord.Epoch, TargetBundleSha256: next.SHA256, TargetCaFingerprints: next.Fingerprints,
			SourceEpoch: epoch, ActiveSince: pgtype.Timestamptz{Time: now.Add(-10 * time.Minute), Valid: true},
			ActiveControlPlaneIds: activeControlPlaneIDs,
		}); seedErr != nil {
			return seedErr
		}
		stable, createErr = fromRecord(stableRecord)
		return createErr
	})
	return stable, err
}

// FailRotation closes a pre-cutover preparing epoch. It intentionally refuses
// overlapping rotations because those require issuer and leaf rollback first.
func (service Service) FailRotation(ctx context.Context, epoch int64, reason string) error {
	record, err := service.Store.Queries.GetTrustBundle(ctx, epoch)
	if err != nil {
		return err
	}
	bundle, err := fromRecord(record)
	if err != nil {
		return err
	}
	if bundle.State != StatePreparing {
		return fmt.Errorf("Trust Bundle epoch %d is %s; issuer rollback is required before it can be aborted", epoch, bundle.State)
	}
	_, err = service.Store.Queries.UpdateTrustBundleState(ctx, db.UpdateTrustBundleStateParams{
		Epoch: epoch, State: StateFailed, BundlePem: string(bundle.Material.PEM), BundleSha256: bundle.Material.SHA256,
		CurrentCaFingerprints: bundle.CurrentCAFingerprints, NextCaFingerprints: bundle.NextCAFingerprints,
		RetireAt: pgtype.Timestamptz{}, LastError: strings.TrimSpace(reason),
	})
	return err
}

// Merge canonicalizes the union of two already validated public CA sets.
func Merge(left, right Material) (Material, error) {
	certificates := make(map[string]*x509.Certificate, len(left.Certificates)+len(right.Certificates))
	for _, certificate := range append(append([]*x509.Certificate{}, left.Certificates...), right.Certificates...) {
		certificates[certificateFingerprint(certificate)] = certificate
	}
	fingerprints := make([]string, 0, len(certificates))
	for fingerprint := range certificates {
		fingerprints = append(fingerprints, fingerprint)
	}
	slices.Sort(fingerprints)
	var value bytes.Buffer
	for _, fingerprint := range fingerprints {
		value.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificates[fingerprint].Raw}))
	}
	return Parse(value.Bytes(), time.Now().UTC())
}

func (material Material) Select(fingerprints []string) (Material, error) {
	wanted := make(map[string]struct{}, len(fingerprints))
	for _, fingerprint := range normalizeFingerprints(fingerprints) {
		wanted[fingerprint] = struct{}{}
	}
	var value bytes.Buffer
	for _, certificate := range material.Certificates {
		if _, ok := wanted[certificateFingerprint(certificate)]; ok {
			value.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}))
		}
	}
	selected, err := Parse(value.Bytes(), time.Now().UTC())
	if err != nil {
		return Material{}, fmt.Errorf("select Trust Bundle certificates: %w", err)
	}
	if !slices.Equal(selected.Fingerprints, normalizeFingerprints(fingerprints)) {
		return Material{}, errors.New("Trust Bundle fingerprint selection is incomplete")
	}
	return selected, nil
}

// Parse validates that input contains only currently valid CA certificates,
// rejects duplicates, and returns deterministic PEM and hashes.
func Parse(value []byte, now time.Time) (Material, error) {
	rest := bytes.TrimSpace(value)
	if len(rest) == 0 {
		return Material{}, errors.New("Trust Bundle is empty")
	}
	seen := map[string]struct{}{}
	certificates := make([]*x509.Certificate, 0, 2)
	blocks := make(map[string][]byte)
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return Material{}, errors.New("Trust Bundle must contain only PEM CERTIFICATE blocks")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return Material{}, fmt.Errorf("parse Trust Bundle certificate: %w", err)
		}
		if !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
			return Material{}, fmt.Errorf("Trust Bundle certificate %q is not a signing CA", certificate.Subject.String())
		}
		if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return Material{}, fmt.Errorf("Trust Bundle certificate %q is not currently valid", certificate.Subject.String())
		}
		fingerprint := certificateFingerprint(certificate)
		if _, exists := seen[fingerprint]; exists {
			return Material{}, fmt.Errorf("Trust Bundle contains duplicate certificate %s", fingerprint)
		}
		seen[fingerprint] = struct{}{}
		certificates = append(certificates, certificate)
		blocks[fingerprint] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
		rest = bytes.TrimSpace(remaining)
	}
	fingerprints := make([]string, 0, len(seen))
	for fingerprint := range seen {
		fingerprints = append(fingerprints, fingerprint)
	}
	slices.Sort(fingerprints)
	var canonical bytes.Buffer
	for _, fingerprint := range fingerprints {
		canonical.Write(blocks[fingerprint])
	}
	digest := sha256.Sum256(canonical.Bytes())
	return Material{PEM: canonical.Bytes(), SHA256: hex.EncodeToString(digest[:]), Fingerprints: fingerprints, Certificates: certificates}, nil
}

func certificateFingerprint(certificate *x509.Certificate) string {
	digest := sha256.Sum256(certificate.Raw)
	return hex.EncodeToString(digest[:])
}

func (service Service) EnsureInitial(ctx context.Context) (Bundle, error) {
	if service.Store == nil || service.MountedPath == "" || service.InitialEpoch < 1 {
		return Bundle{}, errors.New("Trust Bundle service is not configured")
	}
	current, err := service.Store.Queries.GetCurrentTrustBundle(ctx)
	if err == nil {
		return fromRecord(current)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Bundle{}, err
	}
	value, err := os.ReadFile(service.MountedPath)
	if err != nil {
		return Bundle{}, fmt.Errorf("read mounted Trust Bundle: %w", err)
	}
	material, err := Parse(value, time.Now().UTC())
	if err != nil {
		return Bundle{}, err
	}
	now := time.Now().UTC()
	record, err := service.Store.Queries.CreateTrustBundle(ctx, db.CreateTrustBundleParams{
		Epoch: service.InitialEpoch, State: StateStable, BundlePem: string(material.PEM), BundleSha256: material.SHA256,
		CurrentCaFingerprints: material.Fingerprints, NextCaFingerprints: []string{},
		StartedAt: pgtype.Timestamptz{Time: now, Valid: true}, RetireAt: pgtype.Timestamptz{}, LastError: "",
	})
	if err != nil {
		// Multiple replicas may initialize the same fresh deployment.
		current, getErr := service.Store.Queries.GetCurrentTrustBundle(ctx)
		if getErr != nil {
			return Bundle{}, err
		}
		return fromRecord(current)
	}
	return fromRecord(record)
}

func (service Service) Current(ctx context.Context) (Bundle, error) {
	if service.Store == nil {
		return Bundle{}, errors.New("Trust Bundle store is not configured")
	}
	record, err := service.Store.Queries.GetCurrentTrustBundle(ctx)
	if err != nil {
		return Bundle{}, err
	}
	return fromRecord(record)
}

func (service Service) Observe(ctx context.Context, node Node, observed Acknowledgement) (Bundle, bool, error) {
	if err := validateNode(node); err != nil {
		return Bundle{}, false, err
	}
	current, err := service.Current(ctx)
	if err != nil {
		return Bundle{}, false, err
	}
	matched := current.Matches(observed)
	_, err = service.Store.Queries.UpsertNodeTrustPending(ctx, db.UpsertNodeTrustPendingParams{
		NodeKind: node.Kind, NodeID: node.ID, EnterpriseID: node.EnterpriseID, Epoch: current.Epoch,
		BundleSha256: strings.ToLower(observed.SHA256), CaFingerprints: normalizeFingerprints(observed.Fingerprints),
	})
	if err != nil {
		return Bundle{}, false, err
	}
	if matched {
		_, err = service.Store.Queries.AcknowledgeNodeTrustBundle(ctx, db.AcknowledgeNodeTrustBundleParams{
			NodeKind: node.Kind, NodeID: node.ID, Epoch: current.Epoch, BundleSha256: current.Material.SHA256,
			CaFingerprints: current.Material.Fingerprints,
		})
	}
	return current, matched, err
}

func (service Service) Acknowledge(ctx context.Context, node Node, acknowledgement Acknowledgement) error {
	if err := validateNode(node); err != nil {
		return err
	}
	current, err := service.Current(ctx)
	if err != nil {
		return err
	}
	if !current.Matches(acknowledgement) {
		_, _ = service.Store.Queries.FailNodeTrustBundle(ctx, db.FailNodeTrustBundleParams{
			NodeKind: node.Kind, NodeID: node.ID, Epoch: current.Epoch, Error: "TRUST_BUNDLE_ACK_MISMATCH",
		})
		return errors.New("Trust Bundle acknowledgement does not match the current epoch")
	}
	_, err = service.Store.Queries.AcknowledgeNodeTrustBundle(ctx, db.AcknowledgeNodeTrustBundleParams{
		NodeKind: node.Kind, NodeID: node.ID, Epoch: current.Epoch, BundleSha256: current.Material.SHA256,
		CaFingerprints: current.Material.Fingerprints,
	})
	return err
}

// AcknowledgeMounted confirms that a control-plane process can parse the exact
// projected Bundle currently advertised by the database. This is intentionally
// process-level acknowledgement rather than a ConfigMap-exists check.
func (service Service) AcknowledgeMounted(ctx context.Context, nodeID string) error {
	value, err := os.ReadFile(service.MountedPath)
	if err != nil {
		return fmt.Errorf("read mounted Trust Bundle for acknowledgement: %w", err)
	}
	material, err := Parse(value, time.Now().UTC())
	if err != nil {
		return err
	}
	current, err := service.Current(ctx)
	if err != nil {
		return err
	}
	_, matched, err := service.Observe(ctx, Node{Kind: "control_plane", ID: nodeID}, Acknowledgement{
		Epoch: current.Epoch, SHA256: material.SHA256, Fingerprints: material.Fingerprints,
	})
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("mounted Trust Bundle has not reached database epoch %d", current.Epoch)
	}
	return nil
}

// RunMountedAcknowledger follows trust-manager projected updates for the life
// of a process. Transient projection or parse failures are reported and retried;
// the last valid runtime material remains in use by tlsmaterial.
func (service Service) RunMountedAcknowledger(ctx context.Context, nodeID string, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := service.AcknowledgeMounted(ctx, nodeID); err != nil && report != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (bundle Bundle) Matches(value Acknowledgement) bool {
	if bundle.Epoch != value.Epoch || !strings.EqualFold(bundle.Material.SHA256, value.SHA256) {
		return false
	}
	return slices.Equal(bundle.Material.Fingerprints, normalizeFingerprints(value.Fingerprints))
}

func fromRecord(record db.PkiTrustBundle) (Bundle, error) {
	material, err := Parse([]byte(record.BundlePem), time.Now().UTC())
	if err != nil {
		return Bundle{}, fmt.Errorf("stored Trust Bundle epoch %d is invalid: %w", record.Epoch, err)
	}
	if material.SHA256 != record.BundleSha256 || !slices.Equal(material.Fingerprints, normalizeFingerprints(append(append([]string{}, record.CurrentCaFingerprints...), record.NextCaFingerprints...))) {
		return Bundle{}, fmt.Errorf("stored Trust Bundle epoch %d metadata does not match PEM", record.Epoch)
	}
	var retireAt time.Time
	if record.RetireAt.Valid {
		retireAt = record.RetireAt.Time.UTC()
	}
	if record.Direction != DirectionForward && record.Direction != DirectionRollback {
		return Bundle{}, fmt.Errorf("stored Trust Bundle epoch %d has invalid direction %q", record.Epoch, record.Direction)
	}
	return Bundle{Epoch: record.Epoch, State: record.State, Direction: record.Direction, Material: material,
		CurrentCAFingerprints: normalizeFingerprints(record.CurrentCaFingerprints), NextCAFingerprints: normalizeFingerprints(record.NextCaFingerprints),
		StartedAt: record.StartedAt.Time.UTC(), RetireAt: retireAt, LastError: record.LastError}, nil
}

func normalizeFingerprints(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) != 64 {
			continue
		}
		if _, err := hex.DecodeString(value); err != nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func validateNode(node Node) error {
	if !slices.Contains([]string{"connector", "collector", "kubernetes_connector", "control_plane"}, node.Kind) ||
		strings.TrimSpace(node.ID) == "" || len(node.ID) > 256 {
		return errors.New("Trust Bundle node identity is invalid")
	}
	return nil
}
