package server

const (
	EventConnectionEstablished = "pusher:connection_established"
	EventSubscribe             = "pusher:subscribe"
	EventSubscriptionSucceeded = "pusher_internal:subscription_succeeded"
	EventUnsubscribe           = "pusher:unsubscribe"
	EventPing                  = "pusher:ping"
	EventPong                  = "pusher:pong"
	EventError                 = "pusher:error"
)
