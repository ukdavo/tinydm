package auth

import (
	"context"
	"testing"
)

func TestWithPrincipal_PrincipalFromContext_RoundTrip(t *testing.T) {
	p := Principal{
		ID:         "user-42",
		TenantID:   "tenant-99",
		Username:   "bob",
		UserType:   UserTypeUser,
		AuthMethod: AuthMethodBearer,
	}

	ctx := WithPrincipal(context.Background(), p)
	got, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("PrincipalFromContext: expected ok=true, got false")
	}
	if got != p {
		t.Errorf("got %+v, want %+v", got, p)
	}
}

func TestPrincipalFromContext_Missing(t *testing.T) {
	_, ok := PrincipalFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for context with no principal")
	}
}

func TestPrincipalFromContext_ZeroValue(t *testing.T) {
	p, ok := PrincipalFromContext(context.Background())
	if ok {
		t.Error("expected ok=false")
	}
	// Zero value should have empty fields.
	if p.ID != "" || p.Username != "" {
		t.Errorf("expected zero-value principal, got %+v", p)
	}
}

func TestWithPrincipal_DoesNotMutateParent(t *testing.T) {
	parent := context.Background()
	p := Principal{ID: "u1", UserType: UserTypeAdmin}

	child := WithPrincipal(parent, p)

	// Parent context should not carry the principal.
	_, ok := PrincipalFromContext(parent)
	if ok {
		t.Error("parent context should not be modified by WithPrincipal")
	}
	// Child should.
	_, ok = PrincipalFromContext(child)
	if !ok {
		t.Error("child context should carry the principal")
	}
}

func TestPrincipal_IsAdmin(t *testing.T) {
	tests := []struct {
		name      string
		principal Principal
		want      bool
	}{
		{
			name:      "admin user type",
			principal: Principal{UserType: UserTypeAdmin},
			want:      true,
		},
		{
			name:      "superadmin user type",
			principal: Principal{UserType: UserTypeSuperAdmin},
			want:      true,
		},
		{
			name:      "regular user type",
			principal: Principal{UserType: UserTypeUser},
			want:      false,
		},
		{
			name:      "empty user type",
			principal: Principal{},
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.principal.IsAdmin(); got != tc.want {
				t.Errorf("IsAdmin() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWithPrincipal_OverwriteInChildContext(t *testing.T) {
	p1 := Principal{ID: "u1", Username: "alice", UserType: UserTypeUser}
	p2 := Principal{ID: "u2", Username: "admin", UserType: UserTypeAdmin}

	ctx1 := WithPrincipal(context.Background(), p1)
	ctx2 := WithPrincipal(ctx1, p2)

	got1, _ := PrincipalFromContext(ctx1)
	got2, _ := PrincipalFromContext(ctx2)

	if got1.ID != "u1" {
		t.Errorf("ctx1 principal ID: got %q, want %q", got1.ID, "u1")
	}
	if got2.ID != "u2" {
		t.Errorf("ctx2 principal ID: got %q, want %q", got2.ID, "u2")
	}
}
