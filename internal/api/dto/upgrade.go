package dto

import "time"

// RequestUpgradeInput is the request body for POST /api/v1/system/upgrade.
type RequestUpgradeInput struct {
	Body struct {
		Image string `json:"image" minLength:"1" maxLength:"256" doc:"Target Docker image (must match COMPOSER_UPGRADE_IMAGE_PREFIX or be rejected)"`
	}
}

// RequestUpgradeOutput is the response for POST /api/v1/system/upgrade (202).
type RequestUpgradeOutput struct {
	Body struct {
		HelperID       string `json:"helper_id" doc:"Docker container ID of the upgrade helper"`
		FromVersion    string `json:"from_version" doc:"Current Composer version"`
		TargetImage    string `json:"target_image" doc:"Image being upgraded to"`
		DeploymentType string `json:"deployment_type" enum:"compose,docker_run,unknown" doc:"How composer is deployed"`
		StatusURL      string `json:"status_url" doc:"Poll this URL for upgrade progress"`
	}
}

// UpgradeStatusOutput is the response for GET /api/v1/system/upgrade/status.
type UpgradeStatusOutput struct {
	Body struct {
		Status         string    `json:"status" enum:"pending,helper_running,completed,failed" doc:"Current upgrade status"`
		HelperID       string    `json:"helper_id,omitempty" doc:"Docker container ID of the upgrade helper"`
		StartedBy      string    `json:"started_by,omitempty" doc:"User ID or webhook:<id> that triggered the upgrade"`
		FromVersion    string    `json:"from_version" doc:"Current Composer version"`
		TargetImage    string    `json:"target_image" doc:"Image being upgraded to"`
		DeploymentType string    `json:"deployment_type" enum:"compose,docker_run,unknown"`
		ErrorMessage   string    `json:"error_message,omitempty" doc:"Error message if status is failed"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}
}
