package pb_test

import (
	"testing"

	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	"google.golang.org/protobuf/proto"
)

func TestCalendarBindingSerialization(t *testing.T) {
	binding := &pb.CalendarBinding{
		Email:           "test@example.com",
		CalendarId:      "primary",
		AccessToken:     "ya29.access-token-test",
		RefreshToken:    "1//refresh-token-test",
		TokenExpiryUnix: 1710003600,
		LinkedAtUnix:    1710000000,
	}

	data, err := proto.Marshal(binding)
	if err != nil {
		t.Fatalf("proto.Marshal failed: %v", err)
	}

	unmarshaled := &pb.CalendarBinding{}
	if err := proto.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("proto.Unmarshal failed: %v", err)
	}

	if !proto.Equal(binding, unmarshaled) {
		t.Errorf("unmarshaled binding mismatch: got %v, want %v", unmarshaled, binding)
	}

	if unmarshaled.GetEmail() != "test@example.com" {
		t.Errorf("unexpected email: got %s, want test@example.com", unmarshaled.GetEmail())
	}
	if unmarshaled.GetCalendarId() != "primary" {
		t.Errorf("unexpected calendar_id: got %s, want primary", unmarshaled.GetCalendarId())
	}
	if unmarshaled.GetAccessToken() != "ya29.access-token-test" {
		t.Errorf("unexpected access_token: got %s, want ya29.access-token-test", unmarshaled.GetAccessToken())
	}
	if unmarshaled.GetRefreshToken() != "1//refresh-token-test" {
		t.Errorf("unexpected refresh_token: got %s, want 1//refresh-token-test", unmarshaled.GetRefreshToken())
	}
	if unmarshaled.GetTokenExpiryUnix() != 1710003600 {
		t.Errorf("unexpected token_expiry_unix: got %d, want 1710003600", unmarshaled.GetTokenExpiryUnix())
	}
	if unmarshaled.GetLinkedAtUnix() != 1710000000 {
		t.Errorf("unexpected linked_at_unix: got %d, want 1710000000", unmarshaled.GetLinkedAtUnix())
	}
}
