package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/etak64n/deneb/internal/store"
	"golang.org/x/crypto/argon2"
)

// SessionTTL is how long a browser stays signed in.
const SessionTTL = 30 * 24 * time.Hour

// MinPasswordLength is the shortest password of an administrator.
const MinPasswordLength = 12

// The parameters of Argon2id that OWASP recommends: 19 MiB, two passes, one lane.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

// ErrInvalidLogin is returned for a wrong email or password, without telling which.
var ErrInvalidLogin = errors.New("the email or the password is wrong")

// ErrSetupDone is returned by Setup once deneb has an administrator.
var ErrSetupDone = errors.New("deneb has an administrator already")

// ErrInvalidSetupCode is returned by Setup for a wrong setup code.
var ErrInvalidSetupCode = errors.New("the setup code is wrong; deneb writes it to its log when it starts")

// setupAlphabet has the letters and digits that do not look alike, 32 of them, so
// that each random byte picks one evenly.
const setupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newSetupCode returns a code such as "K7QM-2XPA", 40 random bits.
func newSetupCode() string {
	raw := make([]byte, 8)
	rand.Read(raw)
	code := make([]byte, 0, 9)
	for i, b := range raw {
		if i == 4 {
			code = append(code, '-')
		}
		code = append(code, setupAlphabet[int(b)%len(setupAlphabet)])
	}
	return string(code)
}

// SetupCode returns the code that creating the first administrator asks for, or ""
// once there is one. deneb writes it to its log, so that only who runs deneb can
// claim it, and not the first visitor of a server that just started.
func (a *App) SetupCode() string {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	return a.setupCode
}

// normalizeCode lets a setup code be typed in lower case, and with or without its dash.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}

// hashTurn waits until few enough passwords are being hashed, and returns the
// function that ends the turn.
func (a *App) hashTurn(ctx context.Context) (func(), error) {
	select {
	case a.hashing <- struct{}{}:
		return func() { <-a.hashing }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// HashPassword hashes a password with Argon2id, in the PHC string format.
func HashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// CheckPassword tells whether a password matches a hash from HashPassword.
func CheckPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked against when the email is unknown, so that a wrong email
// takes as long as a wrong password and does not reveal which emails exist.
var dummyHash = HashPassword("deneb-dummy-password")

// SetupRequired tells whether deneb has no administrator yet.
func (a *App) SetupRequired(ctx context.Context) (bool, error) {
	count, err := a.store.CountUsers(ctx)
	return count == 0, err
}

// Setup creates the first administrator, with the setup code from the log, and signs
// them in. It fails with ErrSetupDone once one exists.
func (a *App) Setup(ctx context.Context, email, password, code string) (store.User, string, error) {
	want := a.SetupCode()
	if want == "" {
		return store.User{}, "", ErrSetupDone
	}
	if subtle.ConstantTimeCompare([]byte(normalizeCode(code)), []byte(normalizeCode(want))) != 1 {
		return store.User{}, "", ErrInvalidSetupCode
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		return store.User{}, "", errors.New("enter a valid email")
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return store.User{}, "", fmt.Errorf("the password needs at least %d characters", MinPasswordLength)
	}
	done, err := a.hashTurn(ctx)
	if err != nil {
		return store.User{}, "", err
	}
	hash := HashPassword(password)
	done()
	user, err := a.store.CreateFirstUser(ctx, NewID("usr"), email, hash, a.now())
	if errors.Is(err, store.ErrExists) {
		return store.User{}, "", ErrSetupDone
	}
	if err != nil {
		return store.User{}, "", err
	}
	a.setupMu.Lock()
	a.setupCode = ""
	a.setupMu.Unlock()
	token, err := a.startSession(ctx, user.ID)
	return user, token, err
}

// Login checks an email and a password, and returns the token of a new session.
func (a *App) Login(ctx context.Context, email, password string) (store.User, string, error) {
	done, err := a.hashTurn(ctx)
	if err != nil {
		return store.User{}, "", err
	}
	defer done()
	user, err := a.store.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, store.ErrNotFound) {
		CheckPassword(password, dummyHash)
		return store.User{}, "", ErrInvalidLogin
	}
	if err != nil {
		return store.User{}, "", err
	}
	if !CheckPassword(password, user.PasswordHash) {
		return store.User{}, "", ErrInvalidLogin
	}
	token, err := a.startSession(ctx, user.ID)
	return user, token, err
}

func (a *App) startSession(ctx context.Context, userID string) (string, error) {
	token := randomString(32)
	return token, a.store.CreateSession(ctx, HashSecret(token), userID, a.now().Add(SessionTTL))
}

// SessionUser returns the administrator of a session token.
func (a *App) SessionUser(ctx context.Context, token string) (store.User, error) {
	return a.store.SessionUser(ctx, HashSecret(token), a.now())
}

// Logout ends a session.
func (a *App) Logout(ctx context.Context, token string) error {
	return a.store.DeleteSession(ctx, HashSecret(token))
}

// APIKeyPrefix starts every key of the API, so that secret scanners can find leaked keys.
const APIKeyPrefix = "dnb_"

// CreateAPIKey creates a key of the API and returns its secret, which is shown only now.
func (a *App) CreateAPIKey(ctx context.Context, name string) (store.APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return store.APIKey{}, "", errors.New("name the key in 1 to 100 characters")
	}
	random := randomString(32)
	secret := APIKeyPrefix + random
	key, err := a.store.CreateAPIKey(ctx, NewID("key"), name, APIKeyPrefix+random[:6], HashSecret(secret), a.now())
	return key, secret, err
}

// UseAPIKey returns the key of a secret, and records that it was used.
func (a *App) UseAPIKey(ctx context.Context, secret string) (store.APIKey, error) {
	if !strings.HasPrefix(secret, APIKeyPrefix) {
		return store.APIKey{}, store.ErrNotFound
	}
	return a.store.UseAPIKey(ctx, HashSecret(secret), a.now())
}

// CreateWebhook adds an endpoint for the events, with a new secret that signs them.
func (a *App) CreateWebhook(ctx context.Context, url string) (store.Webhook, error) {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return store.Webhook{}, errors.New("the URL must start with https:// or http://")
	}
	return a.store.CreateWebhook(ctx, NewID("wh"), url, "whsec_"+randomString(24), a.now())
}
