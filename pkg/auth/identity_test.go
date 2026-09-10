package auth

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestIdentityFromClaimsPreservesExplicitTenant(t *testing.T) {
	identity := identityFromClaims(&Claims{
		Subject: "user-1", Role: RoleTenant, Tenant: "tenant-a",
		Issuer: "issuer", Audience: "fleet", RequestID: "request-1",
	})
	if identity.Subject != "user-1" || identity.Tenant != "tenant-a" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.Method != MethodHMAC || !identity.HasRole(RoleTenant) {
		t.Fatalf("identity method/roles = %#v", identity)
	}
}

func TestIdentityFromLegacyTenantClaimsUsesSubject(t *testing.T) {
	identity := identityFromClaims(&Claims{Subject: "tenant-a", Role: RoleTenant})
	if identity.Tenant != "tenant-a" {
		t.Fatalf("tenant = %q, want tenant-a", identity.Tenant)
	}
}

func TestIdentityContext(t *testing.T) {
	want := &Identity{Subject: "subject", Roles: []string{RoleViewer}}
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(WithIdentity(context.Background(), want))
	if got := GetIdentity(req); got != want {
		t.Fatalf("GetIdentity() = %#v, want %#v", got, want)
	}
}
