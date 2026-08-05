package domainaccount

import "strings"

type AdminPermission string

const (
	PermissionReviewRead        AdminPermission = "review.read"
	PermissionReviewDecide      AdminPermission = "review.decide"
	PermissionContentEnforce    AdminPermission = "content.enforce"
	PermissionConfigPublish     AdminPermission = "config.publish"
	PermissionGovernanceExecute AdminPermission = "governance.execute"
	PermissionAuditRead         AdminPermission = "audit.read"
)

var registeredAdminPermissions = []AdminPermission{
	PermissionReviewRead,
	PermissionReviewDecide,
	PermissionContentEnforce,
	PermissionConfigPublish,
	PermissionGovernanceExecute,
	PermissionAuditRead,
}

var adminPermissionsByRole = map[string][]AdminPermission{
	RoleReviewer: {
		PermissionReviewRead,
		PermissionReviewDecide,
	},
	RoleOperator: {
		PermissionReviewRead,
		PermissionContentEnforce,
		PermissionConfigPublish,
		PermissionGovernanceExecute,
		PermissionAuditRead,
	},
	RoleAdmin: registeredAdminPermissions,
}

type AdminPrincipal struct {
	UserID int64
	Status int
	Role   string
}

func RestoreAdminPrincipal(userID int64, status int, role string) *AdminPrincipal {
	return &AdminPrincipal{
		UserID: userID,
		Status: status,
		Role:   strings.TrimSpace(role),
	}
}

func RegisteredAdminPermissions() []AdminPermission {
	return append([]AdminPermission(nil), registeredAdminPermissions...)
}

func ValidAdminPermission(permission AdminPermission) bool {
	for _, registered := range registeredAdminPermissions {
		if permission == registered {
			return true
		}
	}
	return false
}

func (p *AdminPrincipal) Active() bool {
	return p != nil && p.UserID > 0 && p.Status == StatusNormal
}

func (p *AdminPrincipal) Permissions() []AdminPermission {
	if !p.Active() {
		return nil
	}
	permissions, ok := adminPermissionsByRole[p.Role]
	if !ok {
		return nil
	}
	return append([]AdminPermission(nil), permissions...)
}

func (p *AdminPrincipal) HasPermission(permission AdminPermission) bool {
	if !ValidAdminPermission(permission) {
		return false
	}
	for _, granted := range p.Permissions() {
		if granted == permission {
			return true
		}
	}
	return false
}
