package server

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/axvilab/wsbro/config"
)

const authTimestampTolerance = 600

// authenticateRequest validates an HTTP API request using Pusher's server-side auth scheme.
// See: https://pusher.com/docs/channels/library_auth_reference/rest-api/#authentication
func (s *Server) authenticateRequest(r *http.Request, body []byte) (*config.App, error) {
	query := r.URL.Query()

	authKey := query.Get("auth_key")
	authTimestamp := query.Get("auth_timestamp")
	authVersion := query.Get("auth_version")
	authSignature := query.Get("auth_signature")

	if authKey == "" || authTimestamp == "" || authSignature == "" {
		return nil, fmt.Errorf("missing auth parameters")
	}

	app := s.Config.FindAppByKey(authKey)
	if app == nil {
		return nil, fmt.Errorf("invalid app key")
	}

	if authVersion != "" && authVersion != "1.0" {
		return nil, fmt.Errorf("unsupported auth version")
	}

	ts, err := strconv.ParseInt(authTimestamp, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp")
	}

	if math.Abs(float64(time.Now().Unix()-ts)) > authTimestampTolerance {
		return nil, fmt.Errorf("timestamp expired")
	}

	// Per Pusher spec, the string to sign is: "METHOD\nPATH\nSORTED_QUERY_PARAMS"
	// auth_signature itself is excluded from the signed params.
	params := make([]string, 0)
	for key, values := range query {
		lowKey := strings.ToLower(key)
		if lowKey == "auth_signature" {
			continue
		}
		for _, val := range values {
			params = append(params, lowKey+"="+val)
		}
	}

	if len(body) > 0 && query.Get("body_md5") == "" {
		params = append(params, "body_md5="+md5Hex(body))
	}

	sort.Strings(params)
	queryString := strings.Join(params, "&")

	stringToSign := fmt.Sprintf("%s\n%s\n%s",
		r.Method,
		r.URL.Path,
		queryString,
	)

	expectedSignature := hmacSHA256([]byte(app.Secret), []byte(stringToSign))

	// Constant-time comparison to prevent timing attacks on the signature.
	if !hmac.Equal([]byte(authSignature), []byte(expectedSignature)) {
		return nil, fmt.Errorf("invalid signature")
	}

	return app, nil
}

func hmacSHA256(key, data []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func md5Hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// validateChannelAuth verifies the client-side auth token for private/presence channel subscriptions.
// Auth format: "app_key:HMAC-SHA256(app_secret, socket_id:channel_name[:channel_data])"
// See: https://pusher.com/docs/channels/library_auth_reference/auth-signatures/#subscription-authentication
func validateChannelAuth(appSecret, socketID string, data SubscribeData) bool {
	if data.Auth == "" {
		return false
	}

	parts := strings.SplitN(data.Auth, ":", 2)
	if len(parts) != 2 {
		return false
	}

	signature := parts[1]

	stringToSign := socketID + ":" + data.Channel
	if data.ChannelData != "" {
		stringToSign += ":" + data.ChannelData
	}

	expectedSignature := hmacSHA256([]byte(appSecret), []byte(stringToSign))

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}
