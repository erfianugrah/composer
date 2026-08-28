package dto

// DockerHostBody is the request body for creating/updating a docker host.
type DockerHostBody struct {
	Name     string `json:"name" minLength:"1" maxLength:"63" doc:"Unique host name (lowercase, dns-label-ish)"`
	Endpoint string `json:"endpoint" minLength:"1" doc:"tcp://host:2376 | tcp://host:2375 | unix:///path.sock"`
	CertDir  string `json:"cert_dir,omitempty" doc:"Directory holding ca.pem/cert.pem/key.pem for mTLS; empty = no TLS"`
}

// --- Request types ---

type CreateHostInput struct {
	Body DockerHostBody
}

type UpdateHostInput struct {
	ID   int64 `path:"id"`
	Body DockerHostBody
}

type HostIDInput struct {
	ID int64 `path:"id"`
}

// HostCertsBody is the request body for PUT /api/v1/hosts/{id}/certs. All
// three PEMs are required and validated (parse, key match, chain to CA)
// before storage.
type HostCertsBody struct {
	CACert string `json:"ca_cert" minLength:"1" doc:"CA certificate PEM (the root the client cert chains to)"`
	Cert   string `json:"cert" minLength:"1" doc:"Client certificate PEM"`
	Key    string `json:"key" minLength:"1" doc:"Client private key PEM (PKCS#1, PKCS#8, or EC)"`
}

// PutHostCertsInput is the input for PUT /api/v1/hosts/{id}/certs.
type PutHostCertsInput struct {
	ID   int64 `path:"id"`
	Body HostCertsBody
}

// --- Response types ---

type DockerHostOutput struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint"`
	CertDir      string `json:"cert_dir,omitempty"`
	TLS          bool   `json:"tls" doc:"true when mTLS material is configured (cert_dir or stored certs)"`
	HasCerts     bool   `json:"has_certs" doc:"true when mTLS certs are stored for this host"`
	CertNotAfter string `json:"cert_not_after,omitempty" doc:"Stored client cert expiry (RFC3339); empty when no certs"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// HostCertsOutput is the metadata-only certificate status for a host. Key
// material is never included.
type HostCertsOutput struct {
	HasCerts    bool   `json:"has_certs"`
	Fingerprint string `json:"fingerprint,omitempty" doc:"sha256 hex of the client cert DER"`
	NotAfter    string `json:"not_after,omitempty" doc:"Client cert expiry (RFC3339)"`
}

type HostCertsOutputBody struct {
	Certs HostCertsOutput `json:"certs"`
}

type GetHostCertsOutput struct {
	Body HostCertsOutputBody
}

type PutHostCertsOutput struct {
	Body HostCertsOutputBody
}

// TestHostOutputBody is the response for POST /api/v1/hosts/{id}/test. Error
// is empty when OK is true; it never contains key material.
type TestHostOutputBody struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty" doc:"Empty on success; connection/ping failure detail otherwise"`
	LatencyMs int64  `json:"latency_ms" doc:"Ping round-trip in milliseconds (0 on failure)"`
}

type TestHostOutput struct {
	Body TestHostOutputBody
}

type HostOutputBody struct {
	Host DockerHostOutput `json:"host"`
}

type ListHostsOutputBody struct {
	Hosts []DockerHostOutput `json:"hosts"`
}

type CreateHostOutput struct {
	Body HostOutputBody
}

type ListHostsOutput struct {
	Body ListHostsOutputBody
}

type GetHostOutput struct {
	Body HostOutputBody
}
