package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestToAuthzRoleExposesBuiltinKey(t *testing.T) {
	now := time.Now().UTC()
	role := toAuthzRole(authorization.RoleRecord{
		Role: db.Role{
			ID:           uuid.New(),
			EnterpriseID: uuid.New(),
			IdentityKey:  pgtype.Text{String: "enterprise_admin", Valid: true},
			Name:         "Enterprise Admin",
			Builtin:      true,
			Status:       "active",
			Version:      1,
			CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		},
	})

	if role.BuiltinKey == nil || *role.BuiltinKey != "enterprise_admin" {
		t.Fatalf("BuiltinKey = %v, want enterprise_admin", role.BuiltinKey)
	}
}

func TestToAuthzRoleOmitsBuiltinKeyForCustomRole(t *testing.T) {
	now := time.Now().UTC()
	role := toAuthzRole(authorization.RoleRecord{
		Role: db.Role{
			ID:           uuid.New(),
			EnterpriseID: uuid.New(),
			Name:         "Production On-call",
			Builtin:      false,
			Status:       "active",
			Version:      1,
			CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		},
	})

	if role.BuiltinKey != nil {
		t.Fatalf("BuiltinKey = %q, want nil", *role.BuiltinKey)
	}
}

func TestHasAllPermissions(t *testing.T) {
	tests := []struct {
		name        string
		permissions []string
		want        bool
	}{
		{name: "both permissions", permissions: []string{"identity.manage", "role.manage"}, want: true},
		{name: "wildcard", permissions: []string{"*"}, want: true},
		{name: "role only", permissions: []string{"role.manage"}, want: false},
		{name: "identity only", permissions: []string{"identity.manage"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			principal := identity.Principal{Permissions: test.permissions}
			if got := hasAllPermissions(principal, "identity.manage", "role.manage"); got != test.want {
				t.Fatalf("hasAllPermissions() = %v, want %v", got, test.want)
			}
		})
	}
}
