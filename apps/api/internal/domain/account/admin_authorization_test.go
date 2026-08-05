package domainaccount

import (
	"reflect"
	"testing"
)

func TestAdminPrincipalPermissions(t *testing.T) {
	tests := []struct {
		name        string
		principal   *AdminPrincipal
		permissions []AdminPermission
	}{
		{
			name:      "reviewer",
			principal: RestoreAdminPrincipal(1, StatusNormal, RoleReviewer),
			permissions: []AdminPermission{
				PermissionReviewRead,
				PermissionReviewDecide,
			},
		},
		{
			name:      "operator",
			principal: RestoreAdminPrincipal(2, StatusNormal, RoleOperator),
			permissions: []AdminPermission{
				PermissionReviewRead,
				PermissionContentEnforce,
				PermissionConfigPublish,
				PermissionGovernanceExecute,
				PermissionAuditRead,
			},
		},
		{
			name:        "compatible admin",
			principal:   RestoreAdminPrincipal(3, StatusNormal, RoleAdmin),
			permissions: RegisteredAdminPermissions(),
		},
		{
			name:        "ordinary user",
			principal:   RestoreAdminPrincipal(4, StatusNormal, RoleUser),
			permissions: nil,
		},
		{
			name:        "unknown role",
			principal:   RestoreAdminPrincipal(5, StatusNormal, "super-admin"),
			permissions: nil,
		},
		{
			name:        "disabled reviewer",
			principal:   RestoreAdminPrincipal(6, 2, RoleReviewer),
			permissions: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.principal.Permissions(); !reflect.DeepEqual(got, tt.permissions) {
				t.Fatalf("Permissions() = %#v, want %#v", got, tt.permissions)
			}
			for _, permission := range RegisteredAdminPermissions() {
				want := containsAdminPermission(tt.permissions, permission)
				if got := tt.principal.HasPermission(permission); got != want {
					t.Fatalf("HasPermission(%q) = %v, want %v", permission, got, want)
				}
			}
		})
	}
}

func TestAdminPermissionRegistryIsClosedAndCopied(t *testing.T) {
	if ValidAdminPermission(AdminPermission("review.override")) {
		t.Fatal("unknown permission must not be registered")
	}
	if RestoreAdminPrincipal(1, StatusNormal, RoleAdmin).HasPermission(AdminPermission("review.override")) {
		t.Fatal("compatible admin must not receive unknown permissions")
	}

	permissions := RegisteredAdminPermissions()
	permissions[0] = AdminPermission("mutated")
	if !ValidAdminPermission(PermissionReviewRead) {
		t.Fatal("callers must not mutate the registered permission set")
	}

	principal := RestoreAdminPrincipal(1, StatusNormal, RoleReviewer)
	granted := principal.Permissions()
	granted[0] = AdminPermission("mutated")
	if !principal.HasPermission(PermissionReviewRead) {
		t.Fatal("callers must not mutate role permission mappings")
	}
}

func containsAdminPermission(permissions []AdminPermission, target AdminPermission) bool {
	for _, permission := range permissions {
		if permission == target {
			return true
		}
	}
	return false
}
