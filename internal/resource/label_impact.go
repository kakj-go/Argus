package resource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sort"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type AffectedSubject struct {
	Type string    `json:"subject_type"`
	ID   uuid.UUID `json:"subject_id"`
}

type LabelImpact struct {
	ResourceType     string            `json:"resource_type"`
	ResourceID       string            `json:"resource_id"`
	BeforeScopeIDs   []string          `json:"before_scope_ids"`
	AfterScopeIDs    []string          `json:"after_scope_ids"`
	AffectedSubjects []AffectedSubject `json:"affected_subjects"`
}

func ComputeLabelImpact(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, resourceType, resourceID string, before, after map[string]string) (LabelImpact, []byte, error) {
	rows, err := q.ListDataScopes(ctx, enterpriseID)
	if err != nil {
		return LabelImpact{}, nil, err
	}
	impact := LabelImpact{ResourceType: resourceType, ResourceID: resourceID, BeforeScopeIDs: []string{}, AfterScopeIDs: []string{}, AffectedSubjects: []AffectedSubject{}}
	affectedScopes := make([]uuid.UUID, 0)
	for _, row := range rows {
		scope := authorization.Scope{ID: row.ID.String(), EnterpriseID: row.EnterpriseID.String(), ResourceTypes: row.ResourceTypes,
			ExplicitResourceIDs: row.ExplicitResourceIds, LabelSelector: row.LabelSelector, Status: row.Status}
		beforeMatch := authorization.ScopeMatches(scope, authorization.Resource{EnterpriseID: enterpriseID.String(), Type: resourceType, ID: resourceID, Labels: before})
		afterMatch := authorization.ScopeMatches(scope, authorization.Resource{EnterpriseID: enterpriseID.String(), Type: resourceType, ID: resourceID, Labels: after})
		if beforeMatch {
			impact.BeforeScopeIDs = append(impact.BeforeScopeIDs, row.ID.String())
		}
		if afterMatch {
			impact.AfterScopeIDs = append(impact.AfterScopeIDs, row.ID.String())
		}
		if beforeMatch != afterMatch {
			affectedScopes = append(affectedScopes, row.ID)
		}
	}
	seen := map[string]bool{}
	for _, scopeID := range affectedScopes {
		subjects, err := q.ListSubjectsForDataScope(ctx, db.ListSubjectsForDataScopeParams{EnterpriseID: enterpriseID, DataScopeID: scopeID})
		if err != nil {
			return LabelImpact{}, nil, err
		}
		for _, subject := range subjects {
			key := subject.SubjectType + "/" + subject.SubjectID.String()
			if !seen[key] {
				seen[key] = true
				impact.AffectedSubjects = append(impact.AffectedSubjects, AffectedSubject{Type: subject.SubjectType, ID: subject.SubjectID})
			}
		}
	}
	sort.Strings(impact.BeforeScopeIDs)
	sort.Strings(impact.AfterScopeIDs)
	sort.Slice(impact.AffectedSubjects, func(i, j int) bool {
		if impact.AffectedSubjects[i].Type == impact.AffectedSubjects[j].Type {
			return impact.AffectedSubjects[i].ID.String() < impact.AffectedSubjects[j].ID.String()
		}
		return impact.AffectedSubjects[i].Type < impact.AffectedSubjects[j].Type
	})
	encoded, err := json.Marshal(impact)
	if err != nil {
		return LabelImpact{}, nil, err
	}
	hash := sha256.Sum256(encoded)
	return impact, hash[:], nil
}

func ApplyLabelImpact(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, impact LabelImpact) error {
	for _, subject := range impact.AffectedSubjects {
		switch subject.Type {
		case "user":
			if _, err := q.BumpUserAuthorizationVersion(ctx, db.BumpUserAuthorizationVersionParams{ID: subject.ID, EnterpriseID: enterpriseID}); err != nil {
				return err
			}
		case "service_account":
			if _, err := q.BumpServiceAccountAuthorizationVersion(ctx, db.BumpServiceAccountAuthorizationVersionParams{ID: subject.ID, EnterpriseID: enterpriseID}); err != nil {
				return err
			}
		}
		if err := q.BumpAuthorizationVersionRecord(ctx, db.BumpAuthorizationVersionRecordParams{EnterpriseID: enterpriseID, SubjectType: subject.Type, SubjectID: subject.ID}); err != nil {
			return err
		}
	}
	return nil
}
