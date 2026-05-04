package domain

import "time"

type User struct {
	Id           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Public struct {
	Id          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

func (u *User) Public() Public {
	return Public{Id: u.Id, Username: u.Username, DisplayName: u.DisplayName}
}
