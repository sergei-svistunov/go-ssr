package model

import (
	"errors"
	"regexp"
	"sync"
)

type Model struct {
	mu              sync.Mutex
	balance         int
	visitorsOnline  int
	userCount       int
	userLastSeen    map[string]int64 // login -> unix timestamp
}

var (
	ErrInvalidLogin      = errors.New("login contains invalid characters")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidAge        = errors.New("invalid age")

	reservedLogins = map[string]struct{}{
		"add":  {},
		"edit": {},
	}

	loginRe = regexp.MustCompile("^[a-zA-Z0-9]+$")
)

// The locks were skipped on purpose for benchmarks in user methods.

func (m *Model) GetUsers() []MockUser {
	return users
}

func (m *Model) GetUserByLogin(login string) *MockUser {
	return userByLogin[login]
}

func (m *Model) AddUser(user MockUser) error {
	if !loginRe.MatchString(user.Login) {
		return ErrInvalidLogin
	}

	if _, exists := userByLogin[user.Login]; exists {
		return ErrUserAlreadyExists
	}
	if _, exists := reservedLogins[user.Login]; exists {
		return ErrUserAlreadyExists
	}

	if user.Age < 18 || user.Age > 120 {
		return ErrInvalidAge
	}

	users = append(users, user)
	userByLogin[user.Login] = &users[len(users)-1]

	m.mu.Lock()
	m.userCount++
	m.mu.Unlock()

	return nil
}

// --- Balance ---

func (m *Model) Balance() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balance
}

func (m *Model) IncBalance(delta int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balance += delta
	return m.balance
}

// --- Visitors Online ---

func (m *Model) VisitorsOnline() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.visitorsOnline
}

func (m *Model) SetVisitorsOnline(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.visitorsOnline = n
}

// --- User Count ---

func (m *Model) UserCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.userCount == 0 {
		return len(users)
	}
	return m.userCount
}

func (m *Model) IncUserCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userCount++
	return m.userCount
}

// --- Per-user last seen ---

func (m *Model) UserLastSeen(login string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.userLastSeen == nil {
		return 0
	}
	return m.userLastSeen[login]
}

func (m *Model) TouchUserLastSeen(login string, ts int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.userLastSeen == nil {
		m.userLastSeen = make(map[string]int64)
	}
	m.userLastSeen[login] = ts
}
