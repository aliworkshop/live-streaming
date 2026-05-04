package domain

import "errors"

var (
	ErrUserExists      = errors.New("user already exists")
	ErrInvalidCreds    = errors.New("invalid username or password")
	ErrUserNotFound    = errors.New("user not found")
	ErrInvalidPassword = errors.New("password must be at least 8 characters")
	ErrInvalidUsername = errors.New("username must be 3-32 characters")
)

type UserUc interface {
	Signup(username, password, displayName string) (*User, string, error)
	Login(username, password string) (*User, string, error)
	GetById(id string) (*User, error)
	List() []Public
}

type Repository interface {
	Save(u *User) error
	FindByUsername(username string) (*User, error)
	FindById(id string) (*User, error)
	All() []*User
}
