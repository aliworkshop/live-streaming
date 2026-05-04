package repository

import (
	"strings"
	"sync"

	"github.com/aliworkshop/live-streaming/user/domain"
)

type memory struct {
	mu       sync.RWMutex
	byId     map[string]*domain.User
	byUsname map[string]*domain.User
}

func NewMemory() domain.Repository {
	return &memory{
		byId:     make(map[string]*domain.User),
		byUsname: make(map[string]*domain.User),
	}
}

func (m *memory) Save(u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(u.Username)
	if _, ok := m.byUsname[key]; ok {
		return domain.ErrUserExists
	}
	m.byId[u.Id] = u
	m.byUsname[key] = u
	return nil
}

func (m *memory) FindByUsername(username string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.byUsname[strings.ToLower(username)]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (m *memory) FindById(id string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.byId[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (m *memory) All() []*domain.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*domain.User, 0, len(m.byId))
	for _, u := range m.byId {
		out = append(out, u)
	}
	return out
}
