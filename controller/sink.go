package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

type SinkRepo struct {
	db *postgres.DB
}

func NewSinkRepo(db *postgres.DB) *SinkRepo {
	return &SinkRepo{
		db: db,
	}
}

func (r *SinkRepo) Add(s *ct.Sink) error {
	if s.ID == "" {
		s.ID = random.UUID()
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.QueryRow("sink_insert", s.ID, s.Kind, []byte(s.Config)).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}
	// create sink event
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      "",
		ObjectID:   s.ID,
		ObjectType: ct.EventTypeSink,
	}, s); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func scanSinks(rows *pgx.Rows) ([]*ct.Sink, error) {
	var sinks []*ct.Sink
	for rows.Next() {
		sink, err := scanSink(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		sinks = append(sinks, sink)
	}
	return sinks, rows.Err()
}

func scanSink(s postgres.Scanner) (*ct.Sink, error) {
	sink := &ct.Sink{}
	err := s.Scan(&sink.ID, &sink.Kind, &sink.Config, &sink.CreatedAt, &sink.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	return sink, err
}

func (r *SinkRepo) Get(id string) (*ct.Sink, error) {
	row := r.db.QueryRow("sink_select", id)
	return scanSink(row)
}

func (r *SinkRepo) List() ([]*ct.Sink, error) {
	rows, err := r.db.Query("sink_list")
	if err != nil {
		return nil, err
	}
	return scanSinks(rows)
}

func (r *SinkRepo) ListSince(since time.Time) ([]*ct.Sink, error) {
	rows, err := r.db.Query("sink_list_since", since)
	if err != nil {
		return nil, err
	}
	return scanSinks(rows)
}

func (r *SinkRepo) Remove(id string) error {
	sink, err := r.Get(id)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.Exec("sink_delete", sink.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	// create sink remove event
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      "",
		ObjectID:   sink.ID,
		ObjectType: ct.EventTypeSinkDeletion,
	}, sink); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Create a new sink
func (c *controllerAPI) CreateSink(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var sink ct.Sink
	if err := httphelper.DecodeJSON(req, &sink); err != nil {
		respondWithError(w, err)
		return
	}

	if err := schema.Validate(&sink); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.sinkRepo.Add(&sink); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &sink)
}

// Get a sink
func (c *controllerAPI) GetSink(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	sink, err := c.sinkRepo.Get(params.ByName("sink_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, sink)
}

// List sinks
func (c *controllerAPI) GetSinks(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		c.streamSinks(ctx, w, req)
		return
	}

	list, err := c.sinkRepo.List()
	if err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) streamSinks(ctx context.Context, w http.ResponseWriter, req *http.Request) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.Sink)
	stream := sse.NewStream(w, ch, l)
	stream.Serve()
	defer func() {
		if err == nil {
			stream.Close()
		} else {
			stream.CloseWithError(err)
		}
	}()

	since, err := time.Parse(time.RFC3339Nano, req.FormValue("since"))
	if err != nil {
		return err
	}

	eventListener, err := c.maybeStartEventListener()
	if err != nil {
		l.Error("error starting event listener")
		return err
	}

	sub, err := eventListener.Subscribe("", []string{string(ct.EventTypeSink), string(ct.EventTypeSinkDeletion)}, "")
	if err != nil {
		return err
	}
	defer sub.Close()

	sinks, err := c.sinkRepo.ListSince(since)
	if err != nil {
		return err
	}
	currentUpdatedAt := since
	for _, sink := range sinks {
		select {
		case <-stream.Done:
			return nil
		case ch <- sink:
			if sink.UpdatedAt.After(currentUpdatedAt) {
				currentUpdatedAt = *sink.UpdatedAt
			}
		}
	}

	select {
	case <-stream.Done:
		return nil
	case ch <- &ct.Sink{}:
	}

	for {
		select {
		case <-stream.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			var sink ct.Sink
			if err := json.Unmarshal(event.Data, &sink); err != nil {
				l.Error("error deserializing sink event", "event.id", event.ID, "err", err)
				continue
			}
			if sink.UpdatedAt.Before(currentUpdatedAt) {
				continue
			}
			if event.ObjectType == ct.EventTypeSinkDeletion {
				sink.Config = nil
			}
			select {
			case <-stream.Done:
				return nil
			case ch <- &sink:
			}
		}
	}
}

// Delete a sink
func (c *controllerAPI) DeleteSink(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	sinkID := params.ByName("sink_id")

	sink, err := c.sinkRepo.Get(sinkID)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if err = c.sinkRepo.Remove(sinkID); err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, sink)
}
