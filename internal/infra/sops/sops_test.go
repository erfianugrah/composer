package sops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSopsEncrypted_Dotenv(t *testing.T) {
	data := []byte(`DB_PASSWORD=ENC[AES256_GCM,data:7Fq7,iv:K1qR,tag:Y5u,type:str]
sops_version=3.9.0
sops_mac=ENC[AES256_GCM,data:abc,iv:def,tag:ghi,type:str]
`)
	assert.True(t, IsSopsEncrypted(data))
}

func TestIsSopsEncrypted_YAML(t *testing.T) {
	data := []byte(`apiVersion: v1
kind: Secret
data:
    password: ENC[AES256_GCM,data:xyz,iv:abc,tag:def,type:str]
sops:
    age:
        - recipient: age1qx...
`)
	assert.True(t, IsSopsEncrypted(data))
}

func TestIsSopsEncrypted_JSON(t *testing.T) {
	data := []byte(`{"password":"ENC[AES256_GCM,data:xyz]","sops":{"version":"3.9.0","mac":"ENC[...]"}}`)
	assert.True(t, IsSopsEncrypted(data))
}

func TestIsSopsEncrypted_Plaintext(t *testing.T) {
	data := []byte(`DB_HOST=localhost
DB_PASSWORD=mysecret
`)
	assert.False(t, IsSopsEncrypted(data))
}

func TestIsSopsEncrypted_Empty(t *testing.T) {
	assert.False(t, IsSopsEncrypted(nil))
	assert.False(t, IsSopsEncrypted([]byte("")))
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{".env", "dotenv"},
		{".env.production", "dotenv"},
		{"secrets.env", "dotenv"},
		{"compose.yaml", "yaml"},
		{"docker-compose.yml", "yaml"},
		{"config.json", "json"},
		{"settings.ini", "ini"},
		{"Dockerfile", "binary"},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.expected, DetectFormat(tt.filename))
		})
	}
}

func TestExtractPrivateKey(t *testing.T) {
	// Standard key file with comments
	content := "# created by age-keygen\n# public key: age1abc\nAGE-SECRET-KEY-1ABCDEF\n"
	assert.Equal(t, "AGE-SECRET-KEY-1ABCDEF", extractPrivateKey(content))

	// Escaped newlines (common in env vars)
	content = `# public key: age1qx\nAGE-SECRET-KEY-13XPR45WDKVLP5N76`
	assert.Equal(t, "AGE-SECRET-KEY-13XPR45WDKVLP5N76", extractPrivateKey(content))

	// Raw key only
	content = "AGE-SECRET-KEY-1ABCDEF"
	assert.Equal(t, "AGE-SECRET-KEY-1ABCDEF", extractPrivateKey(content))

	// Empty / comments only
	assert.Equal(t, "", extractPrivateKey("# just a comment\n"))
	assert.Equal(t, "", extractPrivateKey(""))
}
