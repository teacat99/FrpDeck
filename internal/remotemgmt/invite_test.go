package remotemgmt

import (
	"strings"
	"testing"
	"time"
)

func validInvite() *Invitation {
	return &Invitation{
		NodeName:        "home-pc",
		Addr:            "frps.example.com",
		Port:            7000,
		Protocol:        "tcp",
		TLSEnable:       true,
		FrpsToken:       "S3cret",
		RemoteUser:      "alice",
		Sk:              "stcp-secret",
		UIScheme:        "http",
		ServerProxyName: "frpdeck-mgmt-7",
		MgmtToken:       "eyJhbGciOiJIUzI1NiJ9.placeholder.placeholder",
		IssuedAt:        time.Now(),
		ExpireAt:        time.Now().Add(InvitationTTL),
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := validInvite()
	encoded, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if encoded == "" {
		t.Fatal("encoded payload is empty")
	}
	out, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.NodeName != in.NodeName || out.Addr != in.Addr || out.Port != in.Port {
		t.Errorf("decoded mismatch: got %+v want %+v", out, in)
	}
	if out.MgmtToken != in.MgmtToken {
		t.Errorf("mgmt token round-trip failed")
	}
	if out.V != InvitationVersion {
		t.Errorf("version not propagated: got %d want %d", out.V, InvitationVersion)
	}
}

func TestDecodeStripsWhitespace(t *testing.T) {
	encoded, err := Encode(validInvite())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	wrapped := encoded[:20] + "\n" + encoded[20:40] + "  " + encoded[40:]
	if _, err := Decode(wrapped); err != nil {
		t.Errorf("Decode should tolerate whitespace: %v", err)
	}
}

func TestDecodeRejectsEmpty(t *testing.T) {
	if _, err := Decode(""); err == nil {
		t.Error("expected error for empty input")
	}
	if _, err := Decode("    "); err == nil {
		t.Error("expected error for whitespace-only input")
	}
}

func TestDecodeRejectsBadBase64(t *testing.T) {
	if _, err := Decode("!!!"); err == nil {
		t.Error("expected error for non-base64 input")
	}
}

func TestDecodeRejectsBadJSON(t *testing.T) {
	if _, err := Decode("Zm9vYmFy"); err == nil { // "foobar" base64
		t.Error("expected error for non-JSON payload")
	}
}

func TestDecodeRejectsWrongVersion(t *testing.T) {
	in := validInvite()
	in.V = 999
	encoded, _ := Encode(in)
	if _, err := Decode(encoded); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("expected version error, got %v", err)
	}
}

func TestValidateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*Invitation)
		match string
	}{
		{"empty addr", func(i *Invitation) { i.Addr = "" }, "address"},
		{"bad port", func(i *Invitation) { i.Port = 0 }, "port"},
		{"missing sk", func(i *Invitation) { i.Sk = "" }, "stcp"},
		{"missing token", func(i *Invitation) { i.MgmtToken = "" }, "mgmt"},
		{"missing proxy name", func(i *Invitation) { i.ServerProxyName = "" }, "server_proxy_name"},
		{"missing expire", func(i *Invitation) { i.ExpireAt = time.Time{} }, "expire_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := validInvite()
			tc.mod(inv)
			err := inv.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.match) {
				t.Errorf("got %v, want match %q", err, tc.match)
			}
		})
	}
}

func TestExpired(t *testing.T) {
	in := validInvite()
	in.ExpireAt = time.Now().Add(-1 * time.Minute)
	if !in.Expired(time.Now()) {
		t.Error("expected Expired to be true")
	}
	in.ExpireAt = time.Now().Add(1 * time.Minute)
	if in.Expired(time.Now()) {
		t.Error("expected Expired to be false")
	}
}
