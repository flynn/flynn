package main

import (
	"errors"
	"sync"
)

type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AppRepo struct {
	appNames map[string]*App
	appIDs   map[string]*App
	apps     []*App
	mtx      sync.RWMutex
}

func NewAppRepo() *AppRepo {
	return &AppRepo{
		appNames: make(map[string]*App),
		appIDs:   make(map[string]*App),
	}
}

// - validate
// - set id
// - check name doesn't exist
// - persist
func (r *AppRepo) Create(app *App) error {
	// TODO: actually validate
	if app.Name == "" {
		return errors.New("controller: app name must not be blank")
	}
	app.ID = randomID()
	r.mtx.Lock()
	defer r.mtx.Unlock()

	if _, exists := r.appNames[app.Name]; exists {
		return errors.New("controller: app name is already in use")
	}

	r.appNames[app.Name] = app
	r.appIDs[app.ID] = app
	r.apps = append(r.apps, app)

	return nil
}

func (r *AppRepo) Get(id string) *App {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	app := r.appIDs[id]
	if app == nil {
		return nil
	}
	appCopy := *app
	return &appCopy
}
