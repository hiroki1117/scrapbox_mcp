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
	mu               sync.RWMutex
}

type SessionManager struct {
	sessions sync.Map
	ttl      time.Duration
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

	// Update last access time with proper locking
	session.mu.Lock()
	session.LastAccessAt = time.Now()
	session.mu.Unlock()

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
			session.mu.RLock()
			expired := now.Sub(session.LastAccessAt) > sm.ttl
			session.mu.RUnlock()
			if expired {
				sm.sessions.Delete(key)
			}
			return true
		})
	}
}
