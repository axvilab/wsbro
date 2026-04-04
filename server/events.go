package server

// Pusher protocol event names.
// See: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/#events
const (
	EventConnectionEstablished = "pusher:connection_established"
	EventSubscribe             = "pusher:subscribe"
	EventSubscriptionSucceeded = "pusher_internal:subscription_succeeded"
	EventUnsubscribe           = "pusher:unsubscribe"
	EventPing                  = "pusher:ping"
	EventPong                  = "pusher:pong"
	EventError                 = "pusher:error"
	EventMemberAdded           = "pusher_internal:member_added"
	EventMemberRemoved         = "pusher_internal:member_removed"
)
