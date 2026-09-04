package auth

import (
	"context"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"proxyctl/internal/model"
	"proxyctl/internal/store"
)

func TestHumanAccountsHappyPath(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := NewAccounts(st).WithCost(bcrypt.MinCost)
	ctx := context.Background()
	if _, err := a.SetupFirstAdministrator(ctx, "root", "x", "short", "en"); err == nil {
		t.Fatal("short password must fail")
	}
	admin, err := a.SetupFirstAdministrator(ctx, "rootadmin", "Root", "correct-horse", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Username != "rootadmin" || admin.Role != model.RoleAdministrator || admin.Locale != "ru" {
		t.Fatalf("%+v", admin)
	}
	if _, err := a.SetupFirstAdministrator(ctx, "other", "x", "correct-horse", "en"); err == nil {
		t.Fatal("second setup must fail")
	}
	got, err := a.AuthenticatePassword(ctx, "rootadmin", "correct-horse")
	if err != nil || got.ID != admin.ID {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := a.AuthenticatePassword(ctx, "rootadmin", "wrong-password-long"); err == nil {
		t.Fatal("bad password")
	}
	op, err := a.Create(ctx, model.UserCreateRequest{Username: "ops1", Password: "operator-pw", Role: model.RoleOperator, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	begin, err := a.BeginTOTP(ctx, op.ID)
	if err != nil || begin.Secret == "" {
		t.Fatalf("%+v %v", begin, err)
	}
	pending, err := a.Get(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := decodeTOTPSecret(pending.TOTPPending)
	if err != nil {
		t.Fatal(err)
	}
	code := totpAt(key, a.now())
	rec, err := a.ConfirmTOTP(ctx, op.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Codes) != 10 {
		t.Fatalf("codes %d", len(rec.Codes))
	}
	enabled, err := a.Get(ctx, op.ID)
	if err != nil || !enabled.TOTPEnabled {
		t.Fatal("totp should be enabled")
	}
	if err := a.ConsumeMFA(ctx, enabled, "", rec.Codes[0]); err != nil {
		t.Fatal(err)
	}
	if err := a.ConsumeMFA(ctx, enabled, "", rec.Codes[0]); err == nil {
		t.Fatal("recovery codes are one-time")
	}
	dis := true
	if _, err := a.Patch(ctx, admin.ID, model.UserPatch{Disabled: &dis}); err == nil {
		t.Fatal("cannot disable the last administrator")
	}
	ro := model.RoleReadonly
	if _, err := a.Patch(ctx, admin.ID, model.UserPatch{Role: &ro}); err == nil {
		t.Fatal("cannot demote the last administrator")
	}
}

func TestCanMutateIncludesAdministrator(t *testing.T) {
	if !CanMutate(model.RoleAdministrator) || !CanRead(model.RoleAdministrator) {
		t.Fatal("administrator rbac")
	}
	if CanManageUsers(model.RoleOperator) || !CanManageUsers(model.RoleAdministrator) {
		t.Fatal("user admin")
	}
	if !CanManageTokens(model.RoleOperator) || !CanManageTokens(model.RoleAdministrator) {
		t.Fatal("tokens")
	}
}
