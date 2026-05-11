package auth

import "testing"

func adminPrincipal() Principal {
	return Principal{ID: "admin-1", Username: "admin", UserType: UserTypeAdmin}
}

func superPrincipal() Principal {
	return Principal{ID: "super-1", Username: "super", UserType: UserTypeAdmin}
}

func userPrincipal() Principal {
	return Principal{ID: "user-1", Username: "alice", UserType: UserTypeUser}
}

// ─── Admin bypass ─────────────────────────────────────────────────────────────

func TestCan_AdminAlwaysPermitted(t *testing.T) {
	for _, action := range []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete} {
		if !Can(adminPrincipal(), nil, PermModeExplicit, action, ResourceProject, "proj-1") {
			t.Errorf("admin should always be permitted for action %q", action)
		}
	}
}

func TestCan_SuperAdminAlwaysPermitted(t *testing.T) {
	if !Can(superPrincipal(), nil, PermModeExplicit, ActionDelete, ResourceBucket, "b-1") {
		t.Error("superadmin should always be permitted")
	}
}

func TestCan_AdminWithNoRights(t *testing.T) {
	if !Can(adminPrincipal(), []Right{}, PermModeExplicit, ActionDelete, ResourceBucket, "b-xyz") {
		t.Error("admin with empty rights should still be permitted")
	}
}

// ─── Explicit mode ────────────────────────────────────────────────────────────

func TestCan_Explicit_ExactMatch(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	if !Can(p, rights, PermModeExplicit, ActionRead, ResourceProject, "proj-1") {
		t.Error("exact match should be permitted in explicit mode")
	}
}

func TestCan_Explicit_WildcardMatch(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "bucket", ResourceID: "*", CanRead: true, CanCreate: true}}
	if !Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "any-bucket") {
		t.Error("wildcard should grant read in explicit mode")
	}
	if !Can(p, rights, PermModeExplicit, ActionCreate, ResourceBucket, "another-bucket") {
		t.Error("wildcard should grant create in explicit mode")
	}
}

func TestCan_Explicit_NoMatch_Denied(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	// Wrong resource ID.
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceProject, "proj-999") {
		t.Error("wrong resource ID should be denied")
	}
	// Wrong action.
	if Can(p, rights, PermModeExplicit, ActionDelete, ResourceProject, "proj-1") {
		t.Error("action not in rights should be denied")
	}
	// Wrong resource type.
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "proj-1") {
		t.Error("wrong resource type should be denied")
	}
}

func TestCan_Explicit_EmptyRights_Denied(t *testing.T) {
	if Can(userPrincipal(), []Right{}, PermModeExplicit, ActionRead, ResourceProject, "proj-1") {
		t.Error("empty rights in explicit mode should deny")
	}
}

func TestCan_Explicit_NoAncestorFallback(t *testing.T) {
	p := userPrincipal()
	// Grant on project — should NOT grant bucket access in explicit mode.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	ancestor := ResourceAncestor{Type: ResourceProject, ID: "proj-1"}
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "bucket-1", ancestor) {
		t.Error("explicit mode should not fall back to ancestor grants")
	}
}

func TestCan_Explicit_WildcardDoesNotCrossResourceType(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "*", CanRead: true}}
	if Can(p, rights, PermModeExplicit, ActionRead, ResourceBucket, "any-bucket") {
		t.Error("wildcard on projects should not grant bucket access")
	}
}

// ─── Open mode ────────────────────────────────────────────────────────────────

func TestCan_Open_NoRightExists_Allowed(t *testing.T) {
	p := userPrincipal()
	// No rights at all — open mode defaults to allowed.
	if !Can(p, []Right{}, PermModeOpen, ActionRead, ResourceProject, "proj-1") {
		t.Error("open mode with no rights should allow by default")
	}
}

func TestCan_Open_RightExistsAndGrantsAction_Allowed(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	if !Can(p, rights, PermModeOpen, ActionRead, ResourceProject, "proj-1") {
		t.Error("open mode with explicit grant should allow")
	}
}

func TestCan_Open_RightExistsButNotAction_Denied(t *testing.T) {
	p := userPrincipal()
	// Right exists for proj-1 but only grants read, not delete.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanRead: true}}
	if Can(p, rights, PermModeOpen, ActionDelete, ResourceProject, "proj-1") {
		t.Error("open mode: right exists for resource but action not granted — should deny")
	}
}

func TestCan_Open_UnrelatedRightDoesNotRestrict(t *testing.T) {
	p := userPrincipal()
	// Right exists for proj-2 — proj-1 has no right, so open mode allows proj-1.
	rights := []Right{{ResourceType: "project", ResourceID: "proj-2", CanRead: true}}
	if !Can(p, rights, PermModeOpen, ActionDelete, ResourceProject, "proj-1") {
		t.Error("open mode: unrelated right should not restrict access to other resource")
	}
}

// ─── All actions ──────────────────────────────────────────────────────────────

func TestCan_AllActions(t *testing.T) {
	p := userPrincipal()
	rights := []Right{{ResourceType: "project", ResourceID: "proj-1", CanCreate: true, CanRead: true, CanUpdate: true, CanDelete: true}}
	for _, action := range []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete} {
		if !Can(p, rights, PermModeExplicit, action, ResourceProject, "proj-1") {
			t.Errorf("full rights should permit action %q", action)
		}
	}
}
