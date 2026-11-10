package internal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// Staff sign in on the WMS sign-in page. A successful sign-in sets an HttpOnly
// session cookie; every request then gets its X-WMS-Actor-* headers from that
// session. Actor headers sent by the browser are always removed first.

const (
	sessionCookie   = "wms_session"
	sessionLifetime = 12 * time.Hour
	// After this many wrong passwords for one username from one address,
	// sign-in is refused for lockoutWindow.
	maxFailedLogins = 5
	lockoutWindow   = 15 * time.Minute
)

var ErrInvalidLogin = errors.New("wrong username or password")

// dummyHash is compared when the username doesn't exist, so a wrong username takes
// as long as a wrong password and response times don't reveal which accounts exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), 12)

type StaffUser struct {
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
	Active       bool
}

// HashPassword returns a bcrypt hash suitable for wms_user.password_hash.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("the password must be at least 8 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(h), err
}

func newSessionToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	return token, tokenHash(token), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func cookieSecure() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WMS_COOKIE_SECURE")))
	return v != "false" && v != "0" && v != "no"
}

// loginLimiter slows down password guessing. It is kept in memory: a restart clears it.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func (l *loginLimiter) key(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if fwd := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); fwd != "" {
		host = fwd
	}
	return strings.ToLower(username) + "|" + host
}

func (l *loginLimiter) recent(key string, now time.Time) []time.Time {
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if now.Sub(t) < lockoutWindow {
			kept = append(kept, t)
		}
	}
	l.failures[key] = kept
	return kept
}

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key, time.Now())) >= maxFailedLogins
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Keep memory bounded: once many keys build up, drop the ones with no recent failures.
	if len(l.failures) > 1000 {
		for k := range l.failures {
			if len(l.recent(k, now)) == 0 {
				delete(l.failures, k)
			}
		}
	}
	l.failures[key] = append(l.recent(key, now), now)
}

func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// withSession removes actor headers sent by the browser and, when the request
// carries a valid session cookie, sets them from the signed-in account.
func (h *Handler) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("X-WMS-Actor-ID")
		r.Header.Del("X-WMS-Actor-Name")
		r.Header.Del("X-WMS-Actor-Role")
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			user, err := h.store.SessionUser(r.Context(), tokenHash(c.Value))
			if err == nil && user.Active {
				r.Header.Set("X-WMS-Actor-ID", user.Username)
				r.Header.Set("X-WMS-Actor-Name", user.DisplayName)
				r.Header.Set("X-WMS-Actor-Role", user.Role)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// POST /api/auth/login with {"username":"…","password":"…"}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || input.Password == "" {
		writeError(w, http.StatusBadRequest, "enter your username and password")
		return
	}
	key := h.logins.key(r, input.Username)
	if h.logins.blocked(key) {
		writeError(w, http.StatusTooManyRequests, "too many wrong passwords; try again in 15 minutes")
		return
	}
	user, err := h.store.GetStaffUser(r.Context(), input.Username)
	hashToCheck := []byte(user.PasswordHash)
	if err != nil || !user.Active {
		hashToCheck = dummyHash
	}
	passwordOK := bcrypt.CompareHashAndPassword(hashToCheck, []byte(input.Password)) == nil
	if err != nil || !user.Active || !passwordOK {
		if err != nil && !errors.Is(err, ErrInvalidLogin) {
			log.Printf("login lookup: %v", err)
		}
		h.logins.fail(key)
		time.Sleep(400 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	h.logins.clear(key)
	token, hash, err := newSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start a session")
		return
	}
	expires := time.Now().Add(sessionLifetime)
	if err := h.store.CreateSession(r.Context(), hash, user.Username, expires); err != nil {
		log.Printf("create session: %v", err)
		writeError(w, http.StatusInternalServerError, "could not start a session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: cookieSecure(), SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true,
		"actor": AuthenticatedActor{ID: user.Username, Name: user.DisplayName, Role: user.Role}})
}

// POST /api/auth/logout
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := h.store.DeleteSession(r.Context(), tokenHash(c.Value)); err != nil {
			log.Printf("delete session: %v", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: cookieSecure(), SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

// ---- storage ----

func (s *PostgresStore) GetStaffUser(ctx context.Context, username string) (StaffUser, error) {
	var u StaffUser
	err := s.pool.QueryRow(ctx, `SELECT username,display_name,password_hash,role,active FROM wms_user WHERE lower(username)=lower($1)`, username).
		Scan(&u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrInvalidLogin
	}
	return u, err
}

func (s *PostgresStore) CreateSession(ctx context.Context, hash, username string, expires time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Clean up expired sessions while we are here.
	if _, err := tx.Exec(ctx, `DELETE FROM wms_session WHERE expires_at < now()`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wms_session(token_hash,username,expires_at) VALUES($1,$2,$3)`, hash, username, expires); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE wms_user SET last_login_at=now() WHERE username=$1`, username); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) SessionUser(ctx context.Context, hash string) (StaffUser, error) {
	var u StaffUser
	err := s.pool.QueryRow(ctx, `SELECT u.username,u.display_name,u.role,u.active FROM wms_session s
		JOIN wms_user u ON u.username=s.username WHERE s.token_hash=$1 AND s.expires_at > now()`, hash).
		Scan(&u.Username, &u.DisplayName, &u.Role, &u.Active)
	return u, err
}

func (s *PostgresStore) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM wms_session WHERE token_hash=$1`, hash)
	return err
}

// SaveStaffUser creates or updates an account (used by the "user add" command).
func (s *PostgresStore) SaveStaffUser(ctx context.Context, u StaffUser) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO wms_user(username,display_name,password_hash,role,active)
		VALUES($1,$2,$3,$4,true) ON CONFLICT(username) DO UPDATE SET display_name=EXCLUDED.display_name,
		password_hash=EXCLUDED.password_hash,role=EXCLUDED.role,active=true`, u.Username, u.DisplayName, u.PasswordHash, u.Role)
	if err == nil {
		// A new password signs out every existing session of that account.
		_, err = s.pool.Exec(ctx, `DELETE FROM wms_session WHERE username=$1`, u.Username)
	}
	return err
}

// In-memory versions (used only without DATABASE_URL).

type memoryAuth struct {
	mu       sync.Mutex
	users    map[string]StaffUser
	sessions map[string]memorySession
}

type memorySession struct {
	username string
	expires  time.Time
}

var memAuth = &memoryAuth{users: map[string]StaffUser{}, sessions: map[string]memorySession{}}

func (s *MemoryStore) GetStaffUser(_ context.Context, username string) (StaffUser, error) {
	memAuth.mu.Lock()
	defer memAuth.mu.Unlock()
	u, ok := memAuth.users[strings.ToLower(username)]
	if !ok {
		return u, ErrInvalidLogin
	}
	return u, nil
}

func (s *MemoryStore) CreateSession(_ context.Context, hash, username string, expires time.Time) error {
	memAuth.mu.Lock()
	defer memAuth.mu.Unlock()
	memAuth.sessions[hash] = memorySession{username: username, expires: expires}
	return nil
}

func (s *MemoryStore) SessionUser(_ context.Context, hash string) (StaffUser, error) {
	memAuth.mu.Lock()
	defer memAuth.mu.Unlock()
	sess, ok := memAuth.sessions[hash]
	if !ok || time.Now().After(sess.expires) {
		return StaffUser{}, ErrInvalidLogin
	}
	return memAuth.users[strings.ToLower(sess.username)], nil
}

func (s *MemoryStore) DeleteSession(_ context.Context, hash string) error {
	memAuth.mu.Lock()
	defer memAuth.mu.Unlock()
	delete(memAuth.sessions, hash)
	return nil
}

func (s *MemoryStore) SaveStaffUser(_ context.Context, u StaffUser) error {
	memAuth.mu.Lock()
	defer memAuth.mu.Unlock()
	u.Active = true
	memAuth.users[strings.ToLower(u.Username)] = u
	return nil
}
