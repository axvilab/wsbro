package server

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Pusher protocol events.
// See: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/
const (
	eventConnectionEstablished = "pusher:connection_established"
	eventError                 = "pusher:error"
	eventPing                  = "pusher:ping"
	eventPong                  = "pusher:pong"
	eventSubscribe             = "pusher:subscribe"
	eventUnsubscribe           = "pusher:unsubscribe"
	eventSubscriptionSucceeded = "pusher_internal:subscription_succeeded"
	eventMemberAdded           = "pusher_internal:member_added"
	eventMemberRemoved         = "pusher_internal:member_removed"
	eventCacheMiss             = "pusher:cache_miss"
	eventSignin                = "pusher:signin"
	eventSigninSuccess         = "pusher:signin_success"
)

// userChannelPrefix names the internal channel of a signed-in user. Clients cannot
// subscribe to it ("#" is not allowed in channel names); servers reach it with sendToUser.
const userChannelPrefix = "#server-to-user-"

// Pusher error codes sent to clients.
const (
	errAppDisabled     = 4001 // unknown app key: client must not reconnect
	errOverCapacity    = 4100 // reconnect after a back-off
	errGeneric         = 4200 // reconnect immediately
	errUnauthorized    = 4009
	errRateLimited     = 4301
	errInvalidChannel  = 4302
	errTooManyChannels = 4303
	errInvalidEvent    = 4304
)

const (
	activityTimeoutSeconds = 120
	maxNameLength          = 200
)

// channelNamePattern matches the characters Pusher allows in channel names.
// Event names are free-form (Laravel uses class names such as App\Events\OrderShipped).
var channelNamePattern = regexp.MustCompile(`^[A-Za-z0-9_\-=@,.;:]+$`)

func isValidChannelName(name string) bool {
	return len(name) <= maxNameLength && channelNamePattern.MatchString(name)
}

// isValidTriggerChannel also accepts user channels, which only the HTTP API may publish to.
func isValidTriggerChannel(name string) bool {
	if userID, ok := strings.CutPrefix(name, userChannelPrefix); ok {
		return userID != "" && len(name) <= maxNameLength
	}
	return isValidChannelName(name)
}

func isValidEventName(name string) bool {
	return name != "" && len(name) <= maxNameLength
}

// clientMessage is a message received from a client.
type clientMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// outgoingMessage is a message sent to a client. Data must already be valid JSON.
type outgoingMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	UserID  string          `json:"user_id,omitempty"`
}

// signinData is the payload of pusher:signin.
type signinData struct {
	Auth     string `json:"auth"`
	UserData string `json:"user_data"`
}

// subscribeData is the payload of pusher:subscribe and pusher:unsubscribe.
type subscribeData struct {
	Channel     string `json:"channel"`
	Auth        string `json:"auth"`
	ChannelData string `json:"channel_data"`
}

// encodeEvent builds a protocol event. Pusher sends data as a JSON-encoded string,
// so the payload is marshalled twice: {"data":"{\"key\":\"value\"}"}.
func encodeEvent(event, channel string, data any) []byte {
	inner, _ := json.Marshal(data)
	return encodeStringEvent(event, channel, string(inner))
}

// encodeStringEvent builds an event whose data is an already-serialized string.
func encodeStringEvent(event, channel, data string) []byte {
	quoted, _ := json.Marshal(data)
	msg, _ := json.Marshal(outgoingMessage{Event: event, Channel: channel, Data: quoted})
	return msg
}

// encodeError builds a pusher:error event. Unlike other events, its data is a plain object.
func encodeError(code int, message string) []byte {
	data, _ := json.Marshal(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, message})
	msg, _ := json.Marshal(outgoingMessage{Event: eventError, Data: data})
	return msg
}

// decodeData parses a data field that may be a JSON object or a JSON-encoded string.
// pusher-js sends subscribe data as an object, other clients send a string.
func decodeData(raw json.RawMessage, target any) error {
	if err := json.Unmarshal(raw, target); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	return json.Unmarshal([]byte(s), target)
}

// dataAsString returns the event data as the string Pusher clients expect.
// The HTTP API sends data as a string; a raw JSON object is accepted too and serialized.
func dataAsString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
