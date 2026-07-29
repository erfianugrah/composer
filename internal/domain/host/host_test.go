package host

import "testing"

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		h       Host
		wantErr bool
	}{
		{"valid tcp+mTLS", Host{Name: "remote1", Endpoint: "tcp://docker-remote.example:2376", CertDir: "/certs"}, false},
		{"valid tcp plain", Host{Name: "edge", Endpoint: "tcp://10.0.0.2:2375"}, false},
		{"valid unix socket", Host{Name: "nas", Endpoint: "unix:///run/docker.sock"}, false},
		{"empty name", Host{Name: "", Endpoint: "tcp://x:2375"}, true},
		{"empty endpoint", Host{Name: "x", Endpoint: ""}, true},
		{"bad scheme", Host{Name: "x", Endpoint: "http://x"}, true},
		{"name with space", Host{Name: "my host", Endpoint: "tcp://x:2375"}, true},
		{"reserved name", Host{Name: "local", Endpoint: "tcp://x:2375"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.h.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
