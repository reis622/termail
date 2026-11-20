package ui

import "termail/internal/backend"

// DemoBackendFactory is set by the demo package so the login screen can
// "sign in" without a network in demo mode.
var DemoBackendFactory func(email string) backend.Backend

func demoBackendFor(email string) backend.Backend {
	if DemoBackendFactory != nil {
		return DemoBackendFactory(email)
	}
	return backend.NewMemory([]backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}})
}
