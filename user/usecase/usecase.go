package usecase

import (
	"strings"
	"time"

	"github.com/aliworkshop/live-streaming/user/auth"
	"github.com/aliworkshop/live-streaming/user/domain"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type useCase struct {
	repo  domain.Repository
	token auth.Tokener
}

func New(repo domain.Repository, token auth.Tokener) domain.UserUc {
	return &useCase{repo: repo, token: token}
}

func (uc *useCase) Signup(username, password, displayName string) (*domain.User, string, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 32 {
		return nil, "", domain.ErrInvalidUsername
	}
	if len(password) < 8 {
		return nil, "", domain.ErrInvalidPassword
	}
	if displayName == "" {
		displayName = username
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	u := &domain.User{
		Id:           uuid.NewString(),
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: string(hash),
		CreatedAt:    time.Now().UTC(),
	}
	if err := uc.repo.Save(u); err != nil {
		return nil, "", err
	}
	tok, err := uc.token.Issue(u.Id, u.Username)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}

func (uc *useCase) Login(username, password string) (*domain.User, string, error) {
	u, err := uc.repo.FindByUsername(username)
	if err != nil {
		return nil, "", domain.ErrInvalidCreds
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, "", domain.ErrInvalidCreds
	}
	tok, err := uc.token.Issue(u.Id, u.Username)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}

func (uc *useCase) GetById(id string) (*domain.User, error) {
	return uc.repo.FindById(id)
}

func (uc *useCase) List() []domain.Public {
	users := uc.repo.All()
	out := make([]domain.Public, 0, len(users))
	for _, u := range users {
		out = append(out, u.Public())
	}
	return out
}
