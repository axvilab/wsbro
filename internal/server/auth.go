package server

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const authTimestampTolerance = 600 * time.Second

// verifyAPISignature checks a Pusher REST API request signature.
// See: https://pusher.com/docs/channels/library_auth_reference/rest-api/#authentication
func verifyAPISignature(secret, method, path string, query url.Values, body []byte, now time.Time) error {
	if query.Get("auth_signature") == "" || query.Get("auth_timestamp") == "" {
		return errors.New("missing auth parameters")
	}
	if v := query.Get("auth_version"); v != "" && v != "1.0" {
		return errors.New("unsupported auth_version")
	}

	ts, err := strconv.ParseInt(query.Get("auth_timestamp"), 10, 64)
	if err != nil {
		return errors.New("invalid auth_timestamp")
	}
	if diff := now.Sub(time.Unix(ts, 0)); diff > authTimestampTolerance || diff < -authTimestampTolerance {
		return errors.New("auth_timestamp expired")
	}

	// body_md5 is part of the signed string, so it must match the actual body:
	// otherwise a captured request could be replayed with a different payload.
	if len(body) > 0 && query.Get("body_md5") != md5Hex(body) {
		return errors.New("body_md5 does not match request body")
	}

	params := make([]string, 0, len(query))
	for key, values := range query {
		if key == "auth_signature" {
			continue
		}
		for _, v := range values {
			params = append(params, strings.ToLower(key)+"="+v)
		}
	}
	sort.Strings(params)

	stringToSign := method + "\n" + path + "\n" + strings.Join(params, "&")
	if !hmac.Equal([]byte(query.Get("auth_signature")), []byte(hmacSHA256(secret, stringToSign))) {
		return errors.New("invalid signature")
	}
	return nil
}

// validChannelAuth verifies the token a client sends to join a private or presence channel:
// "app_key:HMAC-SHA256(secret, socket_id:channel[:channel_data])".
// See: https://pusher.com/docs/channels/library_auth_reference/auth-signatures/
func validChannelAuth(key, secret, socketID string, data subscribeData) bool {
	authKey, signature, ok := strings.Cut(data.Auth, ":")
	if !ok || authKey != key {
		return false
	}

	stringToSign := socketID + ":" + data.Channel
	if data.ChannelData != "" {
		stringToSign += ":" + data.ChannelData
	}
	return hmac.Equal([]byte(signature), []byte(hmacSHA256(secret, stringToSign)))
}

// validUserAuth verifies a pusher:signin token: "app_key:HMAC-SHA256(secret, socket_id::user::user_data)".
func validUserAuth(key, secret, socketID string, data signinData) bool {
	authKey, signature, ok := strings.Cut(data.Auth, ":")
	if !ok || authKey != key || data.UserData == "" {
		return false
	}
	expected := hmacSHA256(secret, socketID+"::user::"+data.UserData)
	return hmac.Equal([]byte(signature), []byte(expected))
}

func hmacSHA256(secret, data string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}
