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

// Setup creates the first administrator and signs them in. It fails once one exists.
func (a *App) Setup(ctx context.Context, email, password string) (store.User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") {
		return store.User{}, "", errors.New("enter a valid email")
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return store.User{}, "", fmt.Errorf("the password needs at least %d characters", MinPasswordLength)
	}
	user, err := a.store.CreateFirstUser(ctx, NewID("usr"), email, HashPassword(password), a.now())
	if err != nil {
		return store.User{}, "", err
	}
	token, err := a.startSession(ctx, user.ID)
	return user, token, err
}

// Login checks an email and a password, and returns the token of a new session.
func (a *App) Login(ctx context.Context, email, password string) (store.User, string, error) {
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
