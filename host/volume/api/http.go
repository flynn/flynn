package volumeapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
)

type HTTPAPI struct {
	vman *volumemanager.Manager
}

func NewHTTPAPI(vman *volumemanager.Manager) *HTTPAPI {
	return &HTTPAPI{vman: vman}
}

func (api *HTTPAPI) RegisterRoutes(r *httprouter.Router) {
	r.POST("/storage/providers", api.CreateProvider)
	r.POST("/storage/providers/:provider_id/volumes", api.Create)
	r.GET("/storage/volumes", api.List)
	r.GET("/storage/volumes/:volume_id", api.Inspect)
	r.PUT("/storage/volumes/:volume_id/snapshot", api.Snapshot)
}

func (api *HTTPAPI) CreateProvider(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var provider volume.Provider
	pspec := &volume.ProviderSpec{}
	if err := json.NewDecoder(r.Body).Decode(&pspec); err != nil {
		httphelper.Error(w, err)
		return
	}
	switch pspec.Kind {
	case "zfs":
		config := &zfs.ProviderConfig{}
		if err := json.Unmarshal(pspec.Config, config); err != nil {
			httphelper.Error(w, err)
			return
		}
		var err error
		if provider, err = zfs.NewProvider(config); err != nil {
			httphelper.Error(w, err)
			return
		}
	case "":
		httphelper.Error(w, httphelper.JSONError{
			Code:    httphelper.ValidationError,
			Message: fmt.Sprintf("volume provider 'kind' field must not be blank"),
		})
		return
	default:
		httphelper.Error(w, httphelper.JSONError{
			Code:    httphelper.ValidationError,
			Message: fmt.Sprintf("volume provider kind %q is not known", pspec.Kind),
		})
		return
	}

	if pspec.ID == "" {
		pspec.ID = random.UUID()
	}

	if err := api.vman.AddProvider(pspec.ID, provider); err != nil {
		switch err {
		case volumemanager.ProviderAlreadyExists:
			httphelper.Error(w, httphelper.JSONError{
				Code:    httphelper.ObjectExistsError,
				Message: fmt.Sprintf("provider %q already exists", pspec.ID),
			})
			return
		default:
			httphelper.Error(w, err)
			return
		}
	}

	httphelper.JSON(w, 200, pspec)
}

func (api *HTTPAPI) Create(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	providerID := ps.ByName("provider_id")

	vol, err := api.vman.NewVolumeFromProvider(providerID)
	if err == volumemanager.NoSuchProvider {
		httphelper.Error(w, httphelper.JSONError{
			Code:    httphelper.ObjectNotFoundError,
			Message: fmt.Sprintf("No volume provider by id %q", providerID),
		})
		return
	}

	httphelper.JSON(w, 200, vol.Info())
}

func (api *HTTPAPI) List(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	vols := api.vman.Volumes()
	volList := make([]*volume.Info, 0, len(vols))
	for _, v := range vols {
		volList = append(volList, v.Info())
	}
	httphelper.JSON(w, 200, volList)
}

func (api *HTTPAPI) Inspect(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	volumeID := ps.ByName("volume_id")
	vol := api.vman.GetVolume(volumeID)
	if vol == nil {
		httphelper.Error(w, httphelper.JSONError{
			Code:    httphelper.ObjectNotFoundError,
			Message: fmt.Sprintf("No volume by id %q", volumeID),
		})
		return
	}

	httphelper.JSON(w, 200, vol.Info())
}

func (api *HTTPAPI) Snapshot(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// TODO
}
