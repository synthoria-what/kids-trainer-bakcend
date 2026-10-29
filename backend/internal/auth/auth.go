package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
)

type Service struct { secret []byte; ttl time.Duration }
func New(secret string, ttl time.Duration) *Service { return &Service{secret: []byte(secret), ttl: ttl} }

type Claims struct { UserID int `json:"user_id"`; Role string `json:"role"`; jwt.RegisteredClaims }
func (s *Service) Token(userID int, role string) (string, int64, error) {
	now := time.Now(); exp := now.Add(s.ttl)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: userID, Role: role, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp), IssuedAt: jwt.NewNumericDate(now)}})
	value, err := t.SignedString(s.secret); return value, int64(s.ttl.Seconds()), err
}
func (s *Service) Parse(token string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) { if t.Method != jwt.SigningMethodHS256 { return nil, errors.New("unexpected signing method") }; return s.secret, nil })
	if err != nil || !t.Valid { return nil, errors.New("invalid token") }
	claims, ok := t.Claims.(*Claims); if !ok { return nil, errors.New("invalid claims") }; return claims, nil
}

func HashPassword(password string) (string, error) {
	if len(password) < 8 { return "", errors.New("password must be at least 8 characters") }
	salt := make([]byte, 16); if _, err := rand.Read(salt); err != nil { return "", err }
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return fmt.Sprintf("argon2id$v=19$m=65536,t=1,p=4$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$"); if len(parts) != 5 { return false }
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[3]); expected, err2 := base64.RawStdEncoding.DecodeString(parts[4]); if err1 != nil || err2 != nil { return false }
	actual := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32); return string(actual) == string(expected)
}
func HashCode(code string) string { sum := sha256.Sum256([]byte(code)); return base64.RawURLEncoding.EncodeToString(sum[:]) }
func NewInviteCode() (string, error) { b := make([]byte, 6); if _, err := rand.Read(b); err != nil { return "", err }; return strings.ToUpper(base64.RawURLEncoding.EncodeToString(b)[:8]), nil }
