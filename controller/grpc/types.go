package controller

import (
	ct "github.com/flynn/flynn/controller/types"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
)

// NewApp converts a controller app for use in protobuf
func NewApp(app *ct.App) *App {
	pbApp := &App{
		Id:            app.ID,
		Name:          app.Name,
		Meta:          app.Meta,
		Strategy:      app.Strategy,
		ReleaseId:     app.ReleaseID,
		DeployTimeout: app.DeployTimeout,
	}
	if app.CreatedAt != nil {
		pbApp.CreatedAt, _ = tspb.TimestampProto(app.CreatedAt)
	}
	if app.UpdatedAt != nil {
		pbApp.UpdatedAt, _ = tspb.TimestampProto(app.UpdatedAt)
	}
	return pbApp
}
