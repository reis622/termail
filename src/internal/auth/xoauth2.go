// Package auth implements Microsoft and Google browser sign-in and the
// XOAUTH2 mechanism used by mail servers for token based login.
package auth

import (
	"errors"

	"github.com/emersion/go-sasl"
)

type xoauth2 struct {
	user, token string
	sent        bool
}

// NewXOAUTH2 returns a SASL client for the XOAUTH2 mechanism.
func NewXOAUTH2(user, token string) sasl.Client { return &xoauth2{user: user, token: token} }

func (x *xoauth2) Start() (string, []byte, error) {
	x.sent = true
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

// Next answers the server's error challenge with an empty response so the
// server can finish with a proper failure.
func (x *xoauth2) Next(challenge []byte) ([]byte, error) {
	if len(challenge) > 0 {
		return []byte{}, nil
	}
	return nil, errors.New("beklenmeyen yanıt")
}
