package server

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/axvilab/wsbro/channel"
)

type Server struct {
	Port     string
	Channels map[string]*channel.Channel
	mu       sync.RWMutex
}

func NewServer(port string) *Server {
	return &Server{
		Port:     port,
		Channels: make(map[string]*channel.Channel),
	}
}

func (s *Server) GetOrCreateChannel(name string) *channel.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, exists := s.Channels[name]
	if !exists {
		ch = channel.New(name)
		s.Channels[name] = ch
		log.Printf("Channel created: %s", name)
	}
	return ch
}

func (s *Server) Start() error {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := fmt.Fprintf(w, "WSBro is running on port %s", s.Port)
		if err != nil {
			log.Printf("Error writing response: %v", err)
		}
	})

	http.HandleFunc("/app/", s.HandleWebSocket)
	http.HandleFunc("/apps/", s.HandleBroadcast)

	fmt.Printf("Listening on port %s", s.Port)
	return http.ListenAndServe(fmt.Sprintf(":%s", s.Port), nil)
}
