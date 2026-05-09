package auth

import "testing"

func adminPrincipal() Principal {
	return Principal{
		ID:       "admin-1",
		TenantID: "tenant-1",
		Username: "admin",
		UserType: UserTypeAdmin,
	}
}

func userPrincipal() Principal {
	return Principal{
		ID:       "user-1",
		TenantID: "tenant-1",
		Username: "alice",
		UserType: UserTypeUser,
	}
}

func TestCan_AdminAlwaysPermitted(t *testing.T) {
	p := adminPrincipal()
	actions := []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete}
	for _, action := range actions {
		if !Can(p, nil, action, ResourceProject, "proj-123") {
			t.Errorf("admin should always be permitted for action %q", action)
		}
	}
}

func TestCan_AdminWithNoRights(t *testing.T) {
	// Admin should be permitted even with an empty rights slice.
	if !Can(adminPrincipal(), []Right{}, ActionDelete, ResourceBucket, "bucket-xyz") {
		t.Error("admin with empty rights should still be permitted")
	}
}

func TestCan_UserWithMatchingRight(t *testing.T) {
	p := userPrincipal()
	rights := []Right{
		{
			ResourceType: string(ResourceProject),
			ResourceID:   "proj-123",
			CanRead:      true,
		},
	}

	if !Can(p, rights, ActionRead, ResourceProject, "proj-123") {
		t.Error("user should be permitted to read the specific project")
	}
}

func TestCan_UserWildcardResource(t *testing.T) {
	p := userPrincipal()
	rights := []Right{
		{
			ResourceType: string(ResourceBucket),
			ResourceID:   "*",
			CanRead:      true,
			CanCreate:    true,
		},
	}

	if !Can(p, rights, ActionRead, ResourceBucket, "any-bucket-id") {
		t.Error("wildcard right should grant read on any bucket")
	}
	if !Can(p, rights, ActionCreate, ResourceBucket, "another-bucket") {
		t.Error("wildcard right should grant create on any bucket")
	}
}

func TestCan_UserNoMatchingRight(t *testing.T) {
	p := userPrincipal()
	rights := []Right{
		{
			ResourceType: string(ResourceProject),
			ResourceID:   "proj-123",
			CanRead:      true,
		},
	}

	// Wrong resource ID.
	if Can(p, rights, ActionRead, ResourceProject, "proj-999") {
		t.Error("user should not be permitted for a different resource ID")
	}
	// Right ID but wrong action (CanDelete not set).
	if Can(p, rights, ActionDelete, ResourceProject, "proj-123") {
		t.Error("user should not be permitted for action not in rights")
	}
	// Right action but wrong resource type.
	if Can(p, rights, ActionRead, ResourceBucket, "proj-123") {
		t.Error("user should not be permitted when resource type does not match")
	}
}

func TestCan_UserEmptyRights(t *testing.T) {
	p := userPrincipal()
	if Can(p, []Right{}, ActionRead, ResourceProject, "proj-1") {
		t.Error("user with no rights should not be permitted")
	}
}

func TestCan_AllActions(t *testing.T) {
	p := userPrincipal()
	rights := []Right{
		{
			ResourceType: string(ResourceTenant),
			ResourceID:   "tenant-1",
			CanCreate:    true,
			CanRead:      true,
			CanUpdate:    true,
			CanDelete:    true,
		},
	}

	tests := []struct {
		action Action
	}{
		{ActionCreate},
		{ActionRead},
		{ActionUpdate},
		{ActionDelete},
	}
	for _, tc := range tests {
		if !Can(p, rights, tc.action, ResourceTenant, "tenant-1") {
			t.Errorf("user should be permitted for action %q with full rights", tc.action)
		}
	}
}

func TestCan_WildcardDoesNotOverrideResourceType(t *testing.T) {
	p := userPrincipal()
	// Wildcard on project resources — should NOT grant bucket access.
	rights := []Right{
		{
			ResourceType: string(ResourceProject),
			ResourceID:   "*",
			CanRead:      true,
		},
	}
	if Can(p, rights, ActionRead, ResourceBucket, "any-bucket") {
		t.Error("wildcard on projects should not grant access to buckets")
	}
}
