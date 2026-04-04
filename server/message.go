package server

import "encoding/json"

type Message struct {
	Event   string `json:"event"`
	Data    string `json:"data"`
	Channel string `json:"channel,omitempty"`
}

func NewMessage(event string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	msg := Message{
		Event: event,
		Data:  string(jsonData),
	}

	return json.Marshal(msg)
}
