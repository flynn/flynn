package main

import (
	"time"
)

type PushEvent struct {
	Ref        string      `json:"ref"`
	After      string      `json:"after"`
	Before     string      `json:"before"`
	Commits    []*Commit   `json:"commits"`
	HeadCommit *Commit     `json:"head_commit"`
	Repository *Repository `json:"repository"`
	Pusher     *User       `json:"pusher"`
}

type Commit struct {
	Id        string     `json:"id"`
	Distinct  bool       `json:"distinct"`
	Message   string     `json:"message"`
	Timestamp *time.Time `json:"timestamp"`
	Url       string     `json:"url"`
	Author    *User      `json:"author"`
	Committer *User      `json:"committer"`
	Added     []string   `json:"added"`
	Removed   []string   `json:"removed"`
	Modified  []string   `json:"modified"`
}

type Repository struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Url         string `json:"url"`
	Description string `json:"description"`
}

type User struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}
