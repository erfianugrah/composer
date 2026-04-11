package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateHTTPTarget_BlocksLoopback(t *testing.T) {
	err := validateHTTPTarget("http://127.0.0.1:8080/secret")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestValidateHTTPTarget_BlocksLocalhost(t *testing.T) {
	err := validateHTTPTarget("http://localhost:8080/secret")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestValidateHTTPTarget_BlocksCloudMetadata(t *testing.T) {
	err := validateHTTPTarget("http://169.254.169.254/latest/meta-data/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "private")
}

func TestValidateHTTPTarget_BlocksPrivateRFC1918(t *testing.T) {
	tests := []string{
		"http://10.0.0.1/admin",
		"http://172.16.0.1:9090/",
		"http://192.168.1.1/config",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			err := validateHTTPTarget(url)
			assert.Error(t, err, "should block private IP: %s", url)
		})
	}
}

func TestValidateHTTPTarget_AllowsPublicIPs(t *testing.T) {
	// Use well-known public DNS — just validates the URL parsing + IP check
	// The function does DNS resolution, so this hits the network.
	// Skip in short mode.
	if testing.Short() {
		t.Skip("skips DNS resolution in short mode")
	}
	err := validateHTTPTarget("http://example.com")
	assert.NoError(t, err)
}

func TestValidateHTTPTarget_AllowsPrivateWhenEnvSet(t *testing.T) {
	t.Setenv("COMPOSER_PIPELINE_ALLOW_PRIVATE_IPS", "true")
	err := validateHTTPTarget("http://127.0.0.1:8080")
	assert.NoError(t, err)
}

func TestValidateHTTPTarget_InvalidURL(t *testing.T) {
	err := validateHTTPTarget("://not-a-url")
	assert.Error(t, err)
}

func TestValidateHTTPTarget_UnresolvableHost(t *testing.T) {
	err := validateHTTPTarget("http://this-host-does-not-exist-xyzzy123.invalid/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DNS lookup failed")
}
