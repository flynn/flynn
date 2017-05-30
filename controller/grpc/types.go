package controller

import (
	"time"

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

// ControllerApp converts a protobuf app into a controller app
func (a *App) ControllerApp() *ct.App {
	return &ct.App{
		ID:            a.GetId(),
		Name:          a.GetName(),
		Strategy:      a.GetStrategy(),
		ReleaseID:     a.GetReleaseId(),
		DeployTimeout: a.GetDeployTimeout(),
		CreatedAt:     fromGRPCTime(a.GetCreatedAt()),
		UpdatedAt:     fromGRPCTime(a.GetUpdatedAt()),
	}
}

func fromGRPCTime(in *tspb.Timestamp) *time.Time {
	var t time.Time
	if in == nil {
		return nil
	}
	t := time.Unix(in.Seconds, in.Nanos).UTC()
	return &t
}
