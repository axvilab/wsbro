package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/axvilab/wsbro/channel"
	"github.com/axvilab/wsbro/connection"
	"github.com/rs/zerolog/log"
)

// PresenceData wraps the presence member hash returned in subscription_succeeded for presence channels.
// See: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/#presence-channel-events
type PresenceData struct {
	Presence PresenceInfo `json:"presence"`
}

type PresenceInfo struct {
	Count int                        `json:"count"`
	IDs   []string                   `json:"ids"`
	Hash  map[string]json.RawMessage `json:"hash"`
}

// handlePresenceSubscribe adds a member to a presence channel, notifies existing subscribers,
// and sends the full member list back to the new subscriber.
func (s *Server) handlePresenceSubscribe(ctx context.Context, conn *connection.Connection, ch *channel.Channel, data SubscribeData) {
	var member channel.Member
	if data.ChannelData == "" {
		log.Warn().Msg("Missing channel_data for presence channel")
		errMsg, _ := NewErrorMessage("channel_data is required for presence channels", "4009")
		_ = conn.Send(ctx, errMsg)
		return
	}

	if err := json.Unmarshal([]byte(data.ChannelData), &member); err != nil {
		log.Warn().Err(err).Msg("Invalid channel_data")
		errMsg, _ := NewErrorMessage("Invalid channel_data JSON", "4009")
		_ = conn.Send(ctx, errMsg)
		return
	}

	if member.UserID == "" {
		log.Warn().Msg("Missing user_id in channel_data")
		errMsg, _ := NewErrorMessage("user_id is required in channel_data", "4009")
		_ = conn.Send(ctx, errMsg)
		return
	}

	addedMsg, _ := NewPusherMessage(EventMemberAdded, ch.Name, member)
	ch.Broadcast(ctx, addedMsg, conn.ID)

	ch.SubscribePresence(conn, member)

	members := ch.GetMembers()
	hash := make(map[string]json.RawMessage)
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
		hash[m.UserID] = m.UserInfo
	}

	presenceData := PresenceData{
		Presence: PresenceInfo{
			Count: len(members),
			IDs:   ids,
			Hash:  hash,
		},
	}

	response, _ := NewPusherMessage(EventSubscriptionSucceeded, ch.Name, presenceData)
	_ = conn.Send(ctx, response)
}

// RemoveFromAllChannels unsubscribes a connection from every channel it belongs to,
// broadcasting member_removed for presence channels and firing vacated webhooks for empty channels.
func (s *Server) RemoveFromAllChannels(ctx context.Context, connID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.Channels {
		if strings.HasPrefix(ch.Name, "presence-") {
			member, exists := ch.GetMember(connID)
			if exists {
				removedMsg, _ := NewPusherMessage(EventMemberRemoved, ch.Name, map[string]string{
					"user_id": member.UserID,
				})
				ch.Broadcast(ctx, removedMsg, connID)
				s.sendWebhook(WebhookMemberRemoved, ch.Name, connID, member)
			}
		}

		ch.Unsubscribe(connID)

		if len(ch.Connections) == 0 {
			s.sendWebhook(WebhookChannelVacated, ch.Name, "", nil)
		}
	}
}
