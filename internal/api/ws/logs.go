package ws

import (
	"bufio"
	"encoding/binary"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/swarm"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func AppLogs(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appID := chi.URLParam(r, "appId")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		subject := "hive.progress." + appID
		sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
			_ = conn.WriteMessage(websocket.TextMessage, msg.Data)
		})
		if err != nil {
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "subscribe failed"))
			return
		}
		defer func() { _ = sub.Unsubscribe() }()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}
}

func ContainerLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.URL.Query().Get("name")
		if appName == "" {
			http.Error(w, "name param required", http.StatusBadRequest)
			return
		}
		tail := r.URL.Query().Get("tail")
		if tail == "" {
			tail = "200"
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		sc, err := swarm.NewClient(nil)
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"docker unavailable"}`))
			return
		}
		defer func() { _ = sc.Close() }()

		serviceName := "hive-app-" + appName
		svc, err := sc.GetService(r.Context(), serviceName)
		if err != nil || svc == nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"service not found"}`))
			return
		}

		reader, err := sc.ServiceLogs(r.Context(), svc.ID, tail, true)
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"failed to get logs"}`))
			return
		}
		defer func() { _ = reader.Close() }()

		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
			}
		}()

		go func() {
			br := bufio.NewReader(reader)
			for {
				// Docker log stream uses 8-byte header frames
				header := make([]byte, 8)
				_, err := io.ReadFull(br, header)
				if err != nil {
					return
				}
				size := binary.BigEndian.Uint32(header[4:8])
				if size == 0 {
					continue
				}
				payload := make([]byte, size)
				_, err = io.ReadFull(br, payload)
				if err != nil {
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					return
				}
			}
		}()

		<-done
	}
}
