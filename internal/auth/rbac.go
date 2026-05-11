package auth

// PermMode controls how tenant-wide access is evaluated for principals with no
// explicit right on a given resource.
type PermMode string

const (
	// PermModeExplicit (default): no access unless a right is explicitly granted.
	PermModeExplicit PermMode = "explicit"
	// PermModeOpen: full access unless a right exists and does not grant the action.
	PermModeOpen PermMode = "open"
	// PermModeInherit: like explicit, but a grant on a parent resource cascades
	// to all children (bucket inherits from project, document from bucket, etc.).
	PermModeInherit PermMode = "inherit"
)

// Action represents a permission being checked.
type Action string

const (
	ActionCreate Action = "create"
	ActionRead   Action = "read"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// ResourceType identifies the kind of resource being accessed.
type ResourceType string

const (
	ResourceTenant   ResourceType = "tenant"
	ResourceProject  ResourceType = "project"
	ResourceBucket   ResourceType = "bucket"
	ResourceDocument ResourceType = "document"
)

// ResourceAncestor is a (type, id) pair representing a parent resource in the
// hierarchy. Pass ancestors from nearest to furthest, e.g. bucket then project.
type ResourceAncestor struct {
	Type ResourceType
	ID   string
}

// Can reports whether principal p may perform action on (resourceType, resourceID).
//
// Admin and superadmin principals are always permitted regardless of rights or mode.
//
// For regular users, rights is the slice returned by Store.GetUserRights or
// Store.GetAPIKeyRights. mode controls the default stance when no right is found:
//   - PermModeExplicit: denied unless an exact or wildcard right grants the action.
//   - PermModeOpen: allowed unless a right exists for this resource and doesn't
//     grant the action.
//   - PermModeInherit: like explicit but if no right is found at the target level,
//     the check walks up through ancestors (nearest first).
//
// ancestors is ordered nearest → furthest (e.g. bucket, then project, then tenant).
func Can(p Principal, rights []Right, mode PermMode, action Action, resourceType ResourceType, resourceID string, ancestors ...ResourceAncestor) bool {
	if p.IsAdmin() {
		return true
	}

	switch mode {
	case PermModeOpen:
		return canOpen(rights, action, resourceType, resourceID)
	case PermModeInherit:
		return canInherit(rights, action, resourceType, resourceID, ancestors)
	default: // PermModeExplicit
		return checkRights(rights, action, resourceType, resourceID)
	}
}

// canOpen allows by default unless a right row exists for the resource and does
// not grant the requested action.
func canOpen(rights []Right, action Action, resourceType ResourceType, resourceID string) bool {
	for _, r := range rights {
		if ResourceType(r.ResourceType) != resourceType {
			continue
		}
		if r.ResourceID != resourceID && r.ResourceID != "*" {
			continue
		}
		// A matching right exists — honour its action bits (opt-down).
		return rightGrantsAction(r, action)
	}
	// No right for this resource — open by default.
	return true
}

// canInherit checks the target resource, then walks up ancestors until a right
// is found. If a right row exists at a level (regardless of which actions it
// grants), that level is authoritative — the ancestor walk stops there. If no
// right exists anywhere in the chain, access is denied.
func canInherit(rights []Right, action Action, resourceType ResourceType, resourceID string, ancestors []ResourceAncestor) bool {
	// Check whether any right row exists for the target resource.
	if hasRight(rights, resourceType, resourceID) {
		// A right exists at this level — it is authoritative (no ancestor fallback).
		return checkRights(rights, action, resourceType, resourceID)
	}
	// No right at target level — walk ancestors nearest → furthest.
	for _, anc := range ancestors {
		if hasRight(rights, anc.Type, anc.ID) {
			return checkRights(rights, action, anc.Type, anc.ID)
		}
	}
	return false
}

// hasRight reports whether any right row in the slice matches (resourceType, resourceID)
// or (resourceType, "*"), regardless of which actions it grants.
func hasRight(rights []Right, resourceType ResourceType, resourceID string) bool {
	for _, r := range rights {
		if ResourceType(r.ResourceType) != resourceType {
			continue
		}
		if r.ResourceID == resourceID || r.ResourceID == "*" {
			return true
		}
	}
	return false
}

// checkRights returns true if any right in the slice grants action on
// (resourceType, resourceID) or (resourceType, "*").
func checkRights(rights []Right, action Action, resourceType ResourceType, resourceID string) bool {
	for _, r := range rights {
		if ResourceType(r.ResourceType) != resourceType {
			continue
		}
		if r.ResourceID != resourceID && r.ResourceID != "*" {
			continue
		}
		if rightGrantsAction(r, action) {
			return true
		}
	}
	return false
}

func rightGrantsAction(r Right, action Action) bool {
	switch action {
	case ActionCreate:
		return r.CanCreate
	case ActionRead:
		return r.CanRead
	case ActionUpdate:
		return r.CanUpdate
	case ActionDelete:
		return r.CanDelete
	}
	return false
}
