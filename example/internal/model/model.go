package model

import (
	"errors"
	"regexp"
)

type Model struct{}

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

// The locks were skipped on purpose for benchmarks

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

	return nil
}
