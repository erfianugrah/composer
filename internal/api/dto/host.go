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

// --- Response types ---

type DockerHostOutput struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	CertDir   string `json:"cert_dir,omitempty"`
	TLS       bool   `json:"tls" doc:"true when cert_dir is set"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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
