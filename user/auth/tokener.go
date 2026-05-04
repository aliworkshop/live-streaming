package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	UserId   string `json:"uid"`
	Username string `json:"usr"`
	jwt.RegisteredClaims
}

type Tokener interface {
	Issue(userId, username string) (string, error)
	Verify(token string) (*Claims, error)
}

type tokener struct {
	secret []byte
	expiry time.Duration
}

func NewTokener(secret string, expiry time.Duration) Tokener {
	return &tokener{secret: []byte(secret), expiry: expiry}
}

func (t *tokener) Issue(userId, username string) (string, error) {
	claims := &Claims{
		UserId:   userId,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(t.expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(t.secret)
}

func (t *tokener) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return t.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}
