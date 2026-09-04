package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"transitforge/internal/ident"
	"transitforge/internal/model"
	"transitforge/internal/store"
	"transitforge/internal/validate"
)

const (
	recoveryCodeCount = 10
	totpIssuer        = "TransitForge"
)

type Accounts struct {
	store  store.Store
	cost   int
	issuer string
	now    func() time.Time
}

func NewAccounts(st store.Store) *Accounts {
	return &Accounts{store: st, cost: bcrypt.DefaultCost, issuer: totpIssuer, now: time.Now}
}

func (a *Accounts) WithCost(cost int) *Accounts {
	a.cost = cost
	return a
}

func (a *Accounts) Count(ctx context.Context) (int, error) {
	return a.store.CountUsers(ctx)
}

func (a *Accounts) Get(ctx context.Context, id model.ID) (*model.User, error) {
	return a.store.GetUser(ctx, id)
}

func (a *Accounts) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	return a.store.GetUserByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
}

func (a *Accounts) List(ctx context.Context) ([]model.User, error) {
	return a.store.ListUsers(ctx)
}

func (a *Accounts) Create(ctx context.Context, req model.UserCreateRequest) (*model.User, error) {
	username, err := normalizeUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if err := validate.HumanPassword(req.Password); err != nil {
		return nil, model.Validation(strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	role, err := model.ParseHumanRole(string(req.Role))
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), a.cost)
	if err != nil {
		return nil, err
	}
	now := a.now().UTC()
	u := &model.User{
		ID:           ident.New(),
		Username:     username,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		PasswordHash: string(hash),
		Role:         role,
		Locale:       NormalizeLocale(req.Locale),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := a.store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	return u, nil
}

func (a *Accounts) SetupFirstAdministrator(ctx context.Context, username, displayName, password, locale string) (*model.User, error) {
	n, err := a.store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, model.ErrConflict("initial administrator already exists")
	}
	return a.Create(ctx, model.UserCreateRequest{
		Username:    username,
		DisplayName: displayName,
		Password:    password,
		Role:        model.RoleAdministrator,
		Locale:      locale,
	})
}

func (a *Accounts) Patch(ctx context.Context, id model.ID, patch model.UserPatch) (*model.User, error) {
	u, err := a.store.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.Password != nil {
		if err := validate.HumanPassword(*patch.Password); err != nil {
			return nil, model.Validation(strings.TrimPrefix(err.Error(), "VALIDATION: "))
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*patch.Password), a.cost)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = string(hash)
	}
	if patch.Role != nil {
		role, err := model.ParseHumanRole(string(*patch.Role))
		if err != nil {
			return nil, err
		}
		if u.Role == model.RoleAdministrator && role != model.RoleAdministrator {
			n, err := a.store.CountAdministrators(ctx)
			if err != nil {
				return nil, err
			}
			if n <= 1 {
				return nil, model.ErrConflict("cannot demote the last administrator")
			}
		}
		u.Role = role
	}
	if patch.Locale != nil {
		u.Locale = NormalizeLocale(*patch.Locale)
	}
	if patch.Disabled != nil {
		if u.Role == model.RoleAdministrator && *patch.Disabled {
			n, err := a.store.CountAdministrators(ctx)
			if err != nil {
				return nil, err
			}
			if n <= 1 {
				return nil, model.ErrConflict("cannot disable the last administrator")
			}
		}
		u.Disabled = *patch.Disabled
	}
	u.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateUser(ctx, u); err != nil {
		return nil, err
	}
	u.PasswordHash = ""
	u.TOTPSecret = ""
	u.TOTPPending = ""
	return u, nil
}

func (a *Accounts) AuthenticatePassword(ctx context.Context, username, password string) (*model.User, error) {
	u, err := a.GetByUsername(ctx, username)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, model.Unauthorized("invalid credentials")
		}
		return nil, err
	}
	if u.Disabled {
		return nil, model.Forbidden("account is disabled")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, model.Unauthorized("invalid credentials")
	}
	return u, nil
}

func (a *Accounts) TouchLogin(ctx context.Context, id model.ID) error {
	u, err := a.store.GetUser(ctx, id)
	if err != nil {
		return err
	}
	u.LastLoginAt = a.now().UTC()
	u.UpdatedAt = u.LastLoginAt
	return a.store.UpdateUser(ctx, u)
}

func (a *Accounts) SetLocale(ctx context.Context, id model.ID, locale string) error {
	u, err := a.store.GetUser(ctx, id)
	if err != nil {
		return err
	}
	u.Locale = NormalizeLocale(locale)
	u.UpdatedAt = a.now().UTC()
	return a.store.UpdateUser(ctx, u)
}

func (a *Accounts) BeginTOTP(ctx context.Context, id model.ID) (*model.TOTPBeginResult, error) {
	u, err := a.store.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	secret, err := randomTOTPSecret()
	if err != nil {
		return nil, err
	}
	u.TOTPPending = secret
	u.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateUser(ctx, u); err != nil {
		return nil, err
	}
	return &model.TOTPBeginResult{
		OTPAuthURL: otpAuthURL(a.issuer, u.Username, secret),
		Secret:     secret,
	}, nil
}

func (a *Accounts) ConfirmTOTP(ctx context.Context, id model.ID, code string) (*model.RecoveryCodesResult, error) {
	u, err := a.store.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(u.TOTPPending) == "" {
		return nil, model.Validation("TOTP enrollment has not been started")
	}
	if !verifyTOTP(u.TOTPPending, code, a.now()) {
		return nil, model.Unauthorized("invalid TOTP code")
	}
	u.TOTPSecret = u.TOTPPending
	u.TOTPPending = ""
	u.TOTPEnabled = true
	u.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateUser(ctx, u); err != nil {
		return nil, err
	}
	return a.RotateRecoveryCodes(ctx, id)
}

func (a *Accounts) DisableTOTP(ctx context.Context, id model.ID) error {
	u, err := a.store.GetUser(ctx, id)
	if err != nil {
		return err
	}
	u.TOTPSecret = ""
	u.TOTPPending = ""
	u.TOTPEnabled = false
	u.UpdatedAt = a.now().UTC()
	if err := a.store.UpdateUser(ctx, u); err != nil {
		return err
	}
	return a.store.ReplaceRecoveryCodes(ctx, id, nil)
}

func (a *Accounts) RotateRecoveryCodes(ctx context.Context, id model.ID) (*model.RecoveryCodesResult, error) {
	if _, err := a.store.GetUser(ctx, id); err != nil {
		return nil, err
	}
	codes, hashes, err := generateRecoveryCodes(a.cost)
	if err != nil {
		return nil, err
	}
	if err := a.store.ReplaceRecoveryCodes(ctx, id, hashes); err != nil {
		return nil, err
	}
	return &model.RecoveryCodesResult{Codes: codes}, nil
}

func (a *Accounts) ConsumeMFA(ctx context.Context, u *model.User, totpCode, recoveryCode string) error {
	if u == nil || !u.TOTPEnabled {
		return nil
	}
	totpCode = strings.TrimSpace(totpCode)
	recoveryCode = normalizeRecovery(recoveryCode)
	if totpCode != "" && verifyTOTP(u.TOTPSecret, totpCode, a.now()) {
		return nil
	}
	if recoveryCode == "" {
		return model.Unauthorized("invalid MFA code")
	}
	list, err := a.store.ListRecoveryCodeHashes(ctx, u.ID)
	if err != nil {
		return err
	}
	for _, c := range list {
		if !c.UsedAt.IsZero() {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(c.Hash), []byte(recoveryCode)) == nil {
			return a.store.MarkRecoveryCodeUsed(ctx, c.ID, a.now().UTC())
		}
	}
	return model.Unauthorized("invalid MFA code")
}

func NormalizeLocale(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "ru" || strings.HasPrefix(s, "ru-") || strings.HasPrefix(s, "ru_") {
		return "ru"
	}
	return "en"
}

func normalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if err := validate.Username(s); err != nil {
		return "", model.Validation(strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	return s, nil
}

func generateRecoveryCodes(cost int) (codes []string, hashes []string, err error) {
	codes = make([]string, 0, recoveryCodeCount)
	hashes = make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		b := make([]byte, 5)
		if _, err = rand.Read(b); err != nil {
			return nil, nil, err
		}
		raw := strings.ToLower(hex.EncodeToString(b))
		code := raw[:5] + "-" + raw[5:]
		hash, err := bcrypt.GenerateFromPassword([]byte(code), cost)
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, string(hash))
	}
	return codes, hashes, nil
}

func normalizeRecovery(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
