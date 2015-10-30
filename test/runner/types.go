package main

import (
	"fmt"
	"path"
	"time"
)

type Event interface {
	fmt.Stringer
	Repo() string
	Commit() string
	Branch() string
}

type PushEvent struct {
	Ref        string      `json:"ref"`
	After      string      `json:"after"`
	Before     string      `json:"before"`
	Deleted    bool        `json:"deleted"`
	Commits    []*Commit   `json:"commits"`
	HeadCommit *Commit     `json:"head_commit"`
	Repository *Repository `json:"repository"`
	Pusher     *User       `json:"pusher"`
}

func (e *PushEvent) Repo() string {
	return e.Repository.Name
}

func (e *PushEvent) Commit() string {
	return e.HeadCommit.Id
}

func (e *PushEvent) Branch() string {
	return path.Base(e.Ref)
}

func (e *PushEvent) String() string {
	return fmt.Sprintf(
		"Push of %s[%s] by %s: %s => %s",
		e.Repo(),
		e.Ref,
		e.Pusher.Name,
		e.Before[0:7],
		e.After[0:7],
	)
}

type PullRequestEvent struct {
	Action      string       `json:"action"`
	Number      int          `json:"number"`
	PullRequest *PullRequest `json:"pull_request"`
	Repository  *Repository  `json:"repository"`
	Sender      *PRUser      `json:"sender"`
}

func (e *PullRequestEvent) Repo() string {
	return e.Repository.Name
}

func (e *PullRequestEvent) Commit() string {
	return e.PullRequest.Head.Sha
}

func (e *PullRequestEvent) Branch() string {
	return e.PullRequest.Head.Ref
}

func (e *PullRequestEvent) String() string {
	return fmt.Sprintf(
		"Pull Request %d %s by %s",
		e.Number,
		e.Action,
		e.Sender.Login,
	)
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

type PullRequest struct {
	Url       string     `json:"url"`
	Number    int        `json:"number"`
	State     string     `json:"state"`
	Title     string     `json:"title"`
	User      *PRUser    `json:"user"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	Head      *PRBranch  `json:"head"`
	Base      *PRBranch  `json:"base"`
}

type PRUser struct {
	Login string `json:"login"`
}

type PRBranch struct {
	Label string      `json:"label"`
	Ref   string      `json:"ref"`
	Sha   string      `json:"sha"`
	User  *PRUser     `json:"user"`
	Repo  *Repository `json:"repo"`
}
