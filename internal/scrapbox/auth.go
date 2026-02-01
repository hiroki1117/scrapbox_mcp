package scrapbox

import "net/http"

// Auth handles Scrapbox authentication
type Auth struct {
	sessionCookie string
}

// NewAuth creates a new Auth instance
func NewAuth(sessionCookie string) *Auth {
	return &Auth{
		sessionCookie: sessionCookie,
	}
}

// AddAuthHeaders adds authentication headers to the request
func (a *Auth) AddAuthHeaders(req *http.Request) {
	if a.sessionCookie != "" {
		req.AddCookie(&http.Cookie{
			Name:  "connect.sid",
			Value: a.sessionCookie,
		})
	}
}
