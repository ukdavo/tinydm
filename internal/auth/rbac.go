package auth

// PermMode controls how access is evaluated for principals with no explicit right.
type PermMode string

const (
	// PermModeExplicit: no access unless a right is explicitly granted.
	PermModeExplicit PermMode = "explicit"
	// PermModeOpen: full access unless a right exists and does not grant the action.
	PermModeOpen PermMode = "open"
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
// Admin principals are always permitted regardless of rights or mode.
//
// For regular users, rights is the slice returned by Store.GetUserRights or
// Store.GetAPIKeyRights. mode controls the default stance when no right is found:
//   - PermModeExplicit: denied unless an exact or wildcard right grants the action.
//   - PermModeOpen: allowed unless a right exists for this resource and doesn't
//     grant the action.
func Can(p Principal, rights []Right, mode PermMode, action Action, resourceType ResourceType, resourceID string, ancestors ...ResourceAncestor) bool {
	if p.IsAdmin() {
		return true
	}

	switch mode {
	case PermModeOpen:
		return canOpen(rights, action, resourceType, resourceID)
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
		return rightGrantsAction(r, action)
	}
	return true
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
