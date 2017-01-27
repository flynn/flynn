package volumeapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/inconshreveable/log15.v2"
)

const snapshotContentType = "application/vnd.zfs.snapshot-stream"

type HTTPAPI struct {
	vman *volumemanager.Manager

	cluster atomic.Value // *cluster.Client
}

func NewHTTPAPI(vman *volumemanager.Manager) *HTTPAPI {
	return &HTTPAPI{vman: vman}
}

func (api *HTTPAPI) ConfigureClusterClient(discoverdURL string) {
	disc := discoverd.NewClientWithURL(discoverdURL)
	api.cluster.Store(cluster.NewClientWithServices(disc.Service))
}

func (api *HTTPAPI) RegisterRoutes(r *httprouter.Router) {
	r.POST("/storage/providers", api.CreateProvider)
	r.POST("/storage/providers/:provider_id/volumes", api.Create)
	r.GET("/storage/volumes", api.List)
	r.GET("/storage/volumes/:volume_id", api.Inspect)
	r.DELETE("/storage/volumes/:volume_id", api.Destroy)
	r.PUT("/storage/volumes/:volume_id/snapshot", api.Snapshot)
	// takes host and volID parameters, triggers a send on the remote host and give it a list of snaps already here, and pipes it into recv
	r.POST("/storage/volumes/:volume_id/pull_snapshot", api.Pull)
	// responds with a snapshot stream binary.  only works on snapshots, takes 'haves' parameters, usually called by a node that's servicing a 'pull_snapshot' request
	r.GET("/storage/volumes/:volume_id/send", api.Send)
}

func (api *HTTPAPI) CreateProvider(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pspec := &volume.ProviderSpec{}
	if err := httphelper.DecodeJSON(r, &pspec); err != nil {
		httphelper.Error(w, err)
		return
	}
	if pspec.ID == "" {
		pspec.ID = random.UUID()
	}
	if pspec.Kind == "" {
		httphelper.ValidationError(w, "kind", "must not be blank")
		return
	}
	var provider volume.Provider
	provider, err := volumemanager.NewProvider(pspec)
	if err == volume.UnknownProviderKind {
		httphelper.ValidationError(w, "kind", fmt.Sprintf("%q is not known", pspec.Kind))
		return
	}

	if err := api.vman.AddProvider(pspec.ID, provider); err != nil {
		switch err {
		case volumemanager.ErrProviderExists:
			httphelper.ObjectExistsError(w, fmt.Sprintf("provider %q already exists", pspec.ID))
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

	// decode the volume config from the request, accepting old clients
	// which may not send any data
	var info volume.Info
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil && err != io.EOF {
		httphelper.Error(w, err)
		return
	}
	vol, err := api.vman.NewVolumeFromProvider(providerID, &info)
	if err != nil {
		switch err {
		case volumemanager.ErrNoSuchProvider:
			httphelper.ObjectNotFoundError(w, fmt.Sprintf("no volume provider with id %q", providerID))
			return
		default:
			httphelper.Error(w, err)
			return
		}
	}

	httphelper.JSON(w, 200, vol.Info())
}

func (api *HTTPAPI) List(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		ch := api.vman.Subscribe()
		defer api.vman.Unsubscribe(ch)
		sse.ServeStream(w, ch, log15.New("fn", "streamVolumes"))
		return
	}

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
		httphelper.ObjectNotFoundError(w, fmt.Sprintf("no volume with id %q", volumeID))
		return
	}

	httphelper.JSON(w, 200, vol.Info())
}

func (api *HTTPAPI) Destroy(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	volumeID := ps.ByName("volume_id")
	err := api.vman.DestroyVolume(volumeID)
	if err != nil {
		switch err {
		case volume.ErrNoSuchVolume:
			httphelper.ObjectNotFoundError(w, fmt.Sprintf("no volume with id %q", volumeID))
			return
		default:
			httphelper.Error(w, err)
			return
		}
	}

	w.WriteHeader(200)
}

func (api *HTTPAPI) Snapshot(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	volumeID := ps.ByName("volume_id")
	snap, err := api.vman.CreateSnapshot(volumeID)
	if err != nil {
		switch err {
		case volume.ErrNoSuchVolume:
			httphelper.ObjectNotFoundError(w, fmt.Sprintf("no volume with id %q", volumeID))
			return
		default:
			httphelper.Error(w, err)
			return
		}
	}

	httphelper.JSON(w, 200, snap.Info())
}

func (api *HTTPAPI) Pull(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	cluster := api.cluster.Load().(*cluster.Client)
	if cluster == nil {
		httphelper.ServiceUnavailableError(w, "cluster client is not configured")
		return
	}

	volumeID := ps.ByName("volume_id")

	pull := &volume.PullCoordinate{}
	if err := httphelper.DecodeJSON(r, &pull); err != nil {
		httphelper.Error(w, err)
		return
	}

	hostClient, err := cluster.Host(pull.HostID)
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	haves, err := api.vman.ListHaves(volumeID)
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	reader, err := hostClient.SendSnapshot(pull.SnapshotID, haves)
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	snap, err := api.vman.ReceiveSnapshot(volumeID, reader)
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	httphelper.JSON(w, 200, snap.Info())
}

func (api *HTTPAPI) Send(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	volumeID := ps.ByName("volume_id")

	if !strings.Contains(r.Header.Get("Accept"), snapshotContentType) {
		httphelper.ValidationError(w, "", fmt.Sprintf("must be prepared to accept a content type of %q", snapshotContentType))
		return
	}
	w.Header().Set("Content-Type", snapshotContentType)

	var haves []json.RawMessage
	if err := httphelper.DecodeJSON(r, &haves); err != nil {
		httphelper.Error(w, err)
		return
	}

	err := api.vman.SendSnapshot(volumeID, haves, w)
	if err != nil {
		switch err {
		case volume.ErrNoSuchVolume:
			httphelper.ObjectNotFoundError(w, fmt.Sprintf("no volume with id %q", volumeID))
			return
		default:
			httphelper.Error(w, err)
			return
		}
	}
}
