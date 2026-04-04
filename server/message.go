package server

import "encoding/json"

// Message represents a Pusher protocol message.
// Data is json.RawMessage to support both string-encoded JSON (protocol standard)
// and raw objects (pusher:error).
type Message struct {
	Event   string          `json:"event"`
	Data    json.RawMessage `json:"data"`
	Channel string          `json:"channel,omitempty"`
}

// NewPusherMessage creates a message following Pusher protocol where data is a JSON-encoded string.
// Used for: connection_established, subscription_succeeded, member_added, member_removed.
func NewPusherMessage(event, channel string, data interface{}) ([]byte, error) {
	innerJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Pusher protocol requires data as a JSON-encoded string, not a raw object.
	// e.g. {"key":"val"} becomes the string "{\"key\":\"val\"}"
	dataStr, err := json.Marshal(string(innerJSON))
	if err != nil {
		return nil, err
	}

	msg := struct {
		Event   string          `json:"event"`
		Channel string          `json:"channel,omitempty"`
		Data    json.RawMessage `json:"data"`
	}{
		Event:   event,
		Channel: channel,
		Data:    dataStr,
	}

	return json.Marshal(msg)
}

// NewErrorMessage creates a pusher:error message where data is a raw object (not string-encoded).
func NewErrorMessage(message string, code string) ([]byte, error) {
	msg := struct {
		Event string `json:"event"`
		Data  struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"data"`
	}{
		Event: EventError,
		Data: struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}{
			Message: message,
			Code:    code,
		},
	}

	return json.Marshal(msg)
}

// NewBroadcastMessage creates a message for event delivery to subscribers.
// Data arrives as a pre-encoded JSON string from the HTTP API, so it only needs string-wrapping.
func NewBroadcastMessage(event, channel, data string) ([]byte, error) {
	dataStr, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	msg := struct {
		Event   string          `json:"event"`
		Channel string          `json:"channel,omitempty"`
		Data    json.RawMessage `json:"data"`
	}{
		Event:   event,
		Channel: channel,
		Data:    dataStr,
	}

	return json.Marshal(msg)
}

// UnmarshalData parses the Data field which can be either a JSON string or a JSON object.
// Pusher protocol sends data as JSON-encoded strings, but pusher-js sends subscribe data as objects.
func UnmarshalData(raw json.RawMessage, target interface{}) error {
	if err := json.Unmarshal(raw, target); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	return json.Unmarshal([]byte(s), target)
}
