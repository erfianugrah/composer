package dto

// HealthCheckOutput is the response body for GET /api/v1/system/health.
// Public endpoint used by load balancers and uptime monitors.
type HealthCheckOutput struct {
	Body struct {
		Status  string `json:"status" enum:"healthy" doc:"Always 'healthy' when the process is responding"`
		Version string `json:"version" doc:"Composer semver"`
	}
}
