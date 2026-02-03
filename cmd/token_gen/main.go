package main

import (
	"fmt"

	"github.com/technosupport/ts-vms/internal/tokens"
)

func main() {
	key := "dev-secret-do-not-use-in-prod"
	mgr := tokens.NewManager(key)

	// User: admin (ID from DB ...0002)
	// Tenant: ...0001
	// Role: Admin (Permissions are loaded from DB/Redis? No, Permissions are RBAC).
	// JWT claims usually contain UserID+TenantID. Role/Perms are checked via MW using UserID.

	// I need the exact User UUID.
	// From previous psql: 00000000-0000-0000-0000-000000000002
	userID := "00000000-0000-0000-0000-000000000002"
	tenantID := "00000000-0000-0000-0000-000000000001"

	token, err := mgr.GenerateAccessToken(userID, tenantID)
	if err != nil {
		panic(err)
	}
	fmt.Println(token)
}
