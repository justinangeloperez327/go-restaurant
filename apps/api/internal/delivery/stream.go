package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type eventRecord struct {
	Seq       int64     `json:"seq"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

func (s Service) authorized(ctx context.Context, id, user string) (bool, error) {
	var permitted bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM delivery_jobs d JOIN orders o ON o.id=d.order_id
		WHERE d.id=$1::uuid AND `+readerScope+`)`, id, user).Scan(&permitted)
	return permitted, err
}
func (s Service) stream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "deliveryID")
	if !uuidRE.MatchString(id) {
		fail(w, 400, "invalid delivery ID")
		return
	}
	user := actor(r)
	ok, err := s.authorized(r.Context(), id, user)
	if err != nil {
		databaseError(w, err)
		return
	}
	if !ok {
		fail(w, 404, "delivery not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, 503, "streaming unsupported")
		return
	}
	cursor := int64(0)
	raw := r.Header.Get("Last-Event-ID")
	if raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			fail(w, 400, "invalid event cursor")
			return
		}
		cursor = parsed
	} else {
		if err = s.DB.QueryRow(r.Context(), `SELECT COALESCE(MAX(seq),0) FROM delivery_events
			WHERE delivery_id=$1::uuid`, id).Scan(&cursor); err != nil {
			databaseError(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	_, _ = fmt.Fprint(w, "retry: 3000\nevent: ready\ndata: {}\n\n")
	flusher.Flush()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// Bounded stream with browser-managed reconnect; short-lived access checks
	// prevent a reassigned or deactivated rider keeping the feed indefinitely.
	timeout := time.NewTimer(55 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
			allowed, err := s.authorized(r.Context(), id, user)
			if err != nil || !allowed {
				return
			}
			rows, err := s.DB.Query(r.Context(), `SELECT seq,event_type,created_at
				FROM delivery_events WHERE delivery_id=$1::uuid AND seq>$2
				ORDER BY seq LIMIT 50`, id, cursor)
			if err != nil {
				return
			}
			events := []eventRecord{}
			for rows.Next() {
				var ev eventRecord
				if err := rows.Scan(&ev.Seq, &ev.Type, &ev.CreatedAt); err != nil {
					rows.Close()
					return
				}
				events = append(events, ev)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return
			}
			if len(events) == 0 {
				_, _ = fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
				continue
			}
			for _, event := range events {
				payload, err := json.Marshal(event)
				if err != nil {
					return
				}
				_, err = fmt.Fprintf(w, "id: %d\nevent: delivery-update\ndata: %s\n\n", event.Seq, payload)
				if err != nil {
					return
				}
				cursor = event.Seq
			}
			flusher.Flush()
		}
	}
}
