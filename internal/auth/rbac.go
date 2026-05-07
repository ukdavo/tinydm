package auth

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
	ResourceTenant  ResourceType = "tenant"
	ResourceProject ResourceType = "project"
	ResourceBucket  ResourceType = "bucket"
)

// Can reports whether principal p is allowed to perform action on the given
// resource. Admin principals are always permitted.
//
// For regular users, rights is the slice returned by Store.GetUserRights —
// it includes direct user rights and rights inherited via group membership.
// Rights are checked for an exact resource ID match first, then a wildcard
// ("*") match for all resources of the type.
func Can(p Principal, rights []Right, action Action, resourceType ResourceType, resourceID string) bool {
	if p.IsAdmin() {
		return true
	}

	for _, r := range rights {
		if ResourceType(r.ResourceType) != resourceType {
			continue
		}
		if r.ResourceID != resourceID && r.ResourceID != "*" {
			continue
		}
		switch action {
		case ActionCreate:
			if r.CanCreate {
				return true
			}
		case ActionRead:
			if r.CanRead {
				return true
			}
		case ActionUpdate:
			if r.CanUpdate {
				return true
			}
		case ActionDelete:
			if r.CanDelete {
				return true
			}
		}
	}
	return false
}
