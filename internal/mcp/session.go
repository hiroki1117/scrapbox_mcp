package mcp

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID               string
	CreatedAt        time.Time
	LastAccessAt     time.Time
	InitializeResult *InitializeResult
}

type SessionManager struct {
	sessions sync.Map
	ttl      time.Duration
	mu       sync.Mutex
}

func NewSessionManager(ttl time.Duration) *SessionManager {
	sm := &SessionManager{
		ttl: ttl,
	}

	// Start cleanup goroutine
	go sm.cleanupExpiredSessions()

	return sm
}

func (sm *SessionManager) Create(initResult *InitializeResult) *Session {
	session := &Session{
		ID:               uuid.New().String(),
		CreatedAt:        time.Now(),
		LastAccessAt:     time.Now(),
		InitializeResult: initResult,
	}

	sm.sessions.Store(session.ID, session)
	return session
}

func (sm *SessionManager) Get(sessionID string) (*Session, bool) {
	value, ok := sm.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}

	session := value.(*Session)

	// Update last access time
	session.LastAccessAt = time.Now()
	sm.sessions.Store(sessionID, session)

	return session, true
}

func (sm *SessionManager) Delete(sessionID string) {
	sm.sessions.Delete(sessionID)
}

func (sm *SessionManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		sm.sessions.Range(func(key, value interface{}) bool {
			session := value.(*Session)
			if now.Sub(session.LastAccessAt) > sm.ttl {
				sm.sessions.Delete(key)
			}
			return true
		})
	}
}
