package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"

	"github.com/axvilab/wsbro/connection"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type SubscribeData struct {
	Channel string `json:"channel"`
}

func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	socketID := fmt.Sprintf("%d.%d", rand.Intn(1000000000), rand.Intn(1000000000))
	conn := connection.New(socketID, ws)

	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {
			log.Printf("Error closing WebSocket connection: %v", err)
		}

		log.Printf("WebSocket connection closed")
	}(ws)

	log.Printf("WebSocket connection opened")

	welcome, err := NewMessage(EventConnectionEstablished, map[string]string{
		"socket_id":        socketID,
		"activity_timeout": "120",
	})
	if err != nil {
		log.Printf("Error creating welcome message: %v", err)
		return
	}

	err = conn.Send(welcome)
	if err != nil {
		log.Printf("Error sending welcome message: %v", err)
	}

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Client disconnected: %v", err)
			break
		}

		var message Message
		if err := json.Unmarshal(msg, &message); err != nil {
			log.Printf("Invalid message: %v", err)
			continue
		}

		switch message.Event {
		case EventSubscribe:
			var data SubscribeData
			if err := json.Unmarshal([]byte(message.Data), &data); err != nil {
				log.Printf("Invalid subscribe data: %v", err)
				continue
			}

			ch := s.GetOrCreateChannel(data.Channel)
			ch.Subscribe(conn)
			log.Printf("Client %s subscribed to %s", socketID, data.Channel)

			response, _ := NewMessage(EventSubscriptionSucceeded, map[string]string{
				"channel": data.Channel,
			})
			err := conn.Send(response)
			if err != nil {
				continue
			}
		case EventPing:
			pong, _ := NewMessage(EventPong, nil)
			err := conn.Send(pong)
			if err != nil {
				continue
			}
		}
	}
}
