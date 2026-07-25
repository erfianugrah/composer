package selfupgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	"go.uber.org/zap"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// DefaultImagePrefix is the allowed image prefix for upgrades.
const DefaultImagePrefix = "ghcr.io/erfianugrah/composer"

// ErrInvalidImage is returned when the target image doesn't match the allowed prefix.
var ErrInvalidImage = errors.New("target image must match COMPOSER_UPGRADE_IMAGE_PREFIX")

// UpgradeService orchestrates the self-upgrade process.
type UpgradeService struct {
	repo    *store.UpgradeRepo
	docker  *docker.Client
	dataDir string
	logger  *zap.Logger
}

// NewUpgradeService creates an UpgradeService.
func NewUpgradeService(repo *store.UpgradeRepo, dockerClient *docker.Client, dataDir string, logger *zap.Logger) *UpgradeService {
	return &UpgradeService{
		repo:    repo,
		docker:  dockerClient,
		dataDir: dataDir,
		logger:  logger,
	}
}

// imagePrefix returns the allowed image prefix from env or the default.
func imagePrefix() string {
	if p := os.Getenv("COMPOSER_UPGRADE_IMAGE_PREFIX"); p != "" {
		return p
	}
	return DefaultImagePrefix
}

// validateImage checks the target image matches the allowed prefix.
func validateImage(image string) error {
	prefix := imagePrefix()
	if !strings.HasPrefix(image, prefix) {
		return fmt.Errorf("%w: %s", ErrInvalidImage, prefix)
	}
	return nil
}

// shellQuote single-quote escapes a string for safe inclusion in a shell
// command evaluated by /bin/sh: ' becomes '\”.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// Request initiates a self-upgrade. It writes the singleton row, builds the
// helper container, and launches it detached. startedBy records who/what
// triggered the upgrade (a user ID, or "webhook:<id>") for audit.
// Returns the upgrade row.
func (s *UpgradeService) Request(ctx context.Context, targetImage, startedBy string) (*store.UpgradeRow, error) {
	if err := validateImage(targetImage); err != nil {
		return nil, err
	}

	// Detect deployment type from self container labels.
	selfID := SelfContainerID()
	deployType := DeployUnknown
	var composeInfo *ComposeInfo
	var inspectRaw []byte
	var selfMounts []Mount

	if s.docker != nil && selfID != "" {
		// The mount table translates container-side paths (data dir, stacks
		// dir) into host paths so the helper container can bind-mount the
		// SAME storage (named volumes live under /var/lib/docker/volumes).
		if mps, err := s.docker.ContainerMounts(ctx, selfID); err == nil {
			for _, mp := range mps {
				selfMounts = append(selfMounts, Mount{
					Type:        string(mp.Type),
					Name:        mp.Name,
					Source:      mp.Source,
					Destination: mp.Destination,
				})
			}
		}

		labels, err := s.docker.ContainerLabels(ctx, selfID)
		if err == nil {
			deployType = DetectDeploymentType(labels)
			if deployType == DeployCompose {
				cp, ok := ParseComposeProject(labels)
				if ok && cp.WorkingDir != "" {
					composeInfo = &ComposeInfo{
						ProjectName:     cp.Name,
						WorkingDir:      cp.WorkingDir,
						ConfigFile:      strings.Join(cp.ConfigFiles, ":"),
						EnvironmentFile: cp.EnvironmentFile,
					}
				}
			}
		}

		// For docker-run deployments, grab the raw inspect JSON up-front
		// so we can reconstruct the run spec and pass args to the helper.
		if deployType == DeployDockerRun {
			raw, err := s.docker.InspectRawJSON(ctx, selfID)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("failed to inspect self container for docker-run reconstruction",
						zap.Error(err))
				}
			} else {
				inspectRaw = raw
			}
		}
	}

	// Write the singleton row.
	row := &store.UpgradeRow{
		ID:             1,
		Status:         "pending",
		StartedBy:      startedBy,
		FromVersion:    composer.Version,
		TargetImage:    targetImage,
		DeploymentType: string(deployType),
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		if errors.Is(err, store.ErrUpgradeInFlight) {
			return nil, err
		}
		return nil, fmt.Errorf("persisting upgrade row: %w", err)
	}

	// Build helper container spec.
	helperID, err := s.launchHelper(ctx, targetImage, deployType, composeInfo, inspectRaw, selfMounts)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, "pending", "failed", "", err.Error())
		return nil, fmt.Errorf("launching helper container: %w", err)
	}

	// Transition to helper_running.
	if err := s.repo.UpdateStatus(ctx, "pending", "helper_running", helperID, ""); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to update upgrade status to helper_running", zap.Error(err))
		}
	}

	// Schedule the upgrade-ack sentinel file so the helper can proceed.
	// The plan requires the old composer to write this ~500ms after the HTTP
	// response returns; we write it asynchronously immediately after launch.
	go s.writeAckSentinel()

	row.HelperID = helperID
	row.Status = "helper_running"
	return row, nil
}

// writeAckSentinel writes the upgrade-ack sentinel file that the helper
// container waits for before starting the upgrade. Called as a goroutine
// from Request() so the HTTP response returns first, then the helper
// unblocks and begins the actual upgrade.
func (s *UpgradeService) writeAckSentinel() {
	// Brief delay ensures the HTTP response is flushed before the helper
	// starts work (the helper polls every 1s, so 500ms is safe).
	time.Sleep(500 * time.Millisecond)

	ackPath := filepath.Join(s.dataDir, "upgrade-ack")
	if err := os.WriteFile(ackPath, []byte("ack\n"), 0644); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to write upgrade-ack sentinel", zap.Error(err))
		}
	}
}

// launchHelper creates and starts the detached helper container.
// inspectRaw is the raw container inspect JSON (used for docker-run path).
func (s *UpgradeService) launchHelper(
	ctx context.Context,
	targetImage string,
	deployType DeploymentType,
	composeInfo *ComposeInfo,
	inspectRaw []byte,
	selfMounts []Mount,
) (string, error) {
	// Build the helper script that runs inside the container.
	helperScript := s.buildHelperScript(targetImage, deployType, composeInfo)

	// Build the container configuration.
	config := &container.Config{
		Image:      targetImage,
		Entrypoint: strslice.StrSlice{},
		Cmd:        strslice.StrSlice{"/bin/sh", "-c", helperScript},
		Labels: map[string]string{
			"io.composer.upgrade-helper": "true",
		},
		Env: []string{"COMPOSER_DATA_DIR=" + s.dataDir},
		// The target image's HEALTHCHECK curls composer's HTTP port, which
		// nothing listens on inside the helper - disable it so the helper
		// does not report (unhealthy) while it works.
		Healthcheck: &container.HealthConfig{Test: strslice.StrSlice{"NONE"}},
	}

	hostConfig := &container.HostConfig{
		Binds:       []string{"/var/run/docker.sock:/var/run/docker.sock:rw"},
		AutoRemove:  false,
		NetworkMode: "host",
		RestartPolicy: container.RestartPolicy{
			Name: "no",
		},
	}

	// Mount the data dir (rw) so the helper can read the ack sentinel and
	// docker-run args file. MountBindFor mounts named volumes BY NAME
	// (portable) and translates bind mounts to their host path.
	hostConfig.Binds = append(hostConfig.Binds, MountBindFor(selfMounts, s.dataDir)+":rw")

	// If stacks dir is set, mount it too (for compose deployments that
	// reference stack directories), with the same translation.
	stacksDir := os.Getenv("COMPOSER_STACKS_DIR")
	if stacksDir != "" {
		hostConfig.Binds = append(hostConfig.Binds, MountBindFor(selfMounts, stacksDir)+":rw")
	}

	// --- Deployment-type-specific setup ---

	switch deployType {
	case DeployCompose:
		if composeInfo == nil {
			return "", errors.New("compose deployment detected but compose info is nil")
		}
		// Bind mount compose working-dir and each config/env file's parent
		// directory so the helper's `docker compose` can resolve relative
		// paths. All mounts are read-only - the helper only reads these files.
		dirSet := map[string]bool{}
		dirSet[composeInfo.WorkingDir] = true
		for _, cf := range strings.Split(composeInfo.ConfigFile, ":") {
			if cf != "" {
				dirSet[filepath.Dir(cf)] = true
			}
		}
		if composeInfo.EnvironmentFile != "" {
			dirSet[filepath.Dir(composeInfo.EnvironmentFile)] = true
		}
		for dir := range dirSet {
			hostConfig.Binds = append(hostConfig.Binds, dir+":"+dir+":ro")
		}

		// Pass compose info as env vars.
		config.Env = append(config.Env,
			"COMPOSER_WORKING_DIR="+composeInfo.WorkingDir,
			"COMPOSER_CONFIG_FILE="+composeInfo.ConfigFile,
			"COMPOSER_PROJECT_NAME="+composeInfo.ProjectName,
		)
		if composeInfo.EnvironmentFile != "" {
			config.Env = append(config.Env, "COMPOSER_ENV_FILE="+composeInfo.EnvironmentFile)
		}

	case DeployDockerRun:
		if len(inspectRaw) == 0 {
			return "", errors.New("docker-run deployment detected but no inspect data available")
		}
		spec, err := ReconstructRunSpec(inspectRaw)
		if err != nil {
			return "", fmt.Errorf("reconstructing run spec: %w", err)
		}
		args, err := spec.DockerRunArgs(targetImage)
		if err != nil {
			return "", fmt.Errorf("building docker run args: %w", err)
		}
		// Give the new container a 35s stop grace for future upgrades
		// (matches deploy/compose.yaml stop_grace_period). The image is the
		// final arg, so insert before it.
		withTimeout := make([]string, 0, len(args)+2)
		withTimeout = append(withTimeout, args[:len(args)-1]...)
		withTimeout = append(withTimeout, "--stop-timeout", "35", args[len(args)-1])
		// Write the run args to a file in the data dir (already mounted into
		// the helper) so the helper script can eval them. Each arg is
		// single-quote escaped: env/label values containing spaces or shell
		// metacharacters must survive the eval verbatim.
		quoted := make([]string, len(withTimeout))
		for i, a := range withTimeout {
			quoted[i] = shellQuote(a)
		}
		argsContent := strings.Join(quoted, " ")
		argsPath := filepath.Join(s.dataDir, "upgrade-docker-run-args")
		if err := os.WriteFile(argsPath, []byte(argsContent), 0600); err != nil {
			return "", fmt.Errorf("writing docker-run-args file: %w", err)
		}
		// Also pass the old container name so the helper can stop+rm it.
		config.Env = append(config.Env, "COMPOSER_OLD_NAME="+spec.Name)

	case DeployUnknown:
		return "", errors.New("unknown deployment type - cannot determine how to upgrade")
	}

	// Create the container.
	containerID, err := s.docker.ContainerCreate(ctx, config, hostConfig, "")
	if err != nil {
		return "", fmt.Errorf("creating helper container: %w", err)
	}

	// Start it.
	if err := s.docker.StartContainer(ctx, containerID); err != nil {
		// Clean up on failure.
		s.docker.ContainerRemove(ctx, containerID, true)
		return "", fmt.Errorf("starting helper container: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("upgrade helper container started",
			zap.String("helper_id", containerID[:12]),
			zap.String("target_image", targetImage),
			zap.String("deployment_type", string(deployType)),
		)
	}

	return containerID, nil
}

// buildHelperScript constructs the shell script the helper container executes.
func (s *UpgradeService) buildHelperScript(targetImage string, deployType DeploymentType, composeInfo *ComposeInfo) string {
	var sb strings.Builder

	// Shared preamble.
	sb.WriteString(`#!/bin/sh
set -e

echo "upgrade helper started (target: $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo unknown) daemon), waiting for ack sentinel"

# Wait for the upgrade-ack sentinel file to appear (composerd writes it
# after returning the HTTP response and releasing the port).
while [ ! -f "$COMPOSER_DATA_DIR/upgrade-ack" ]; do
	sleep 1
done
# Remove the sentinel so a retry upgrade starts clean.
rm -f "$COMPOSER_DATA_DIR/upgrade-ack"

# Function: poll a container's health status until healthy or timeout.
health_poll() {
	container_name="$1"
	max_secs="${2:-120}"
	elapsed=0
	while [ "$elapsed" -lt "$max_secs" ]; do
		status=$(docker inspect --format='{{.State.Health.Status}}' "$container_name" 2>/dev/null || echo "notfound")
		case "$status" in
			healthy)
				echo "container $container_name is healthy"
				return 0
				;;
			unhealthy)
				echo "container $container_name is unhealthy" >&2
				return 1
				;;
		esac
		sleep 2
		elapsed=$((elapsed + 2))
	done
	echo "timeout waiting for $container_name to become healthy" >&2
	return 1
}

`)

	switch deployType {
	case DeployCompose:
		// COMPOSE_FILE natively accepts a colon-separated file list on
		// Linux - no flag assembly, no IFS pitfalls.
		sb.WriteString(`# COMPOSE_FILE takes a colon-separated list natively; the service
# already joined the config file paths with ':'.
export COMPOSE_FILE="$COMPOSER_CONFIG_FILE"

# --env-file only when a non-default env file was used at deploy time.
ENV_FILE_FLAG=""
if [ -n "$COMPOSER_ENV_FILE" ]; then
	ENV_FILE_FLAG="--env-file=$COMPOSER_ENV_FILE"
fi

cd "$COMPOSER_WORKING_DIR" || { echo "working dir $COMPOSER_WORKING_DIR not found" >&2; exit 1; }

echo "pulling image and recreating composer service..."
set -x
docker compose --project-directory "$COMPOSER_WORKING_DIR" \
	$ENV_FILE_FLAG \
	-p "$COMPOSER_PROJECT_NAME" \
	up -d --no-build --remove-orphans --quiet-pull composer
set +x

# Resolve the new composer container by project+service labels - compose
# container names are <project>-<service>-<index>, not the bare service name.
NEW_ID=""
for _i in $(seq 1 15); do
	NEW_ID=$(docker ps -q --filter "label=com.docker.compose.project=$COMPOSER_PROJECT_NAME" --filter "label=com.docker.compose.service=composer" | head -1)
	[ -n "$NEW_ID" ] && break
	sleep 2
done
if [ -z "$NEW_ID" ]; then
	echo "new composer container not found after recreate" >&2
	exit 1
fi
health_poll "$NEW_ID" 120
`)

	case DeployDockerRun:
		sb.WriteString(`# Stop and remove the old composer container before creating the new one.
# create-before-stop fails with published ports (port-already-allocated),
# so stop+remove first is the only safe order.
echo "stopping old composer container..."
docker stop -t 35 "$COMPOSER_OLD_NAME" 2>/dev/null || true
echo "removing old composer container..."
docker rm "$COMPOSER_OLD_NAME" 2>/dev/null || true

echo "starting new composer container..."
eval docker run -d $(cat "$COMPOSER_DATA_DIR/upgrade-docker-run-args")

health_poll "$COMPOSER_OLD_NAME" 120
`)

	default:
		sb.WriteString(`echo "unknown deployment type" >&2
exit 1
`)
	}

	sb.WriteString("\necho \"upgrade completed\"\n")
	return sb.String()
}

// Status returns the current upgrade status.
func (s *UpgradeService) Status(ctx context.Context) (*store.UpgradeRow, error) {
	return s.repo.Get(ctx)
}

// ReconcileAtBoot reconciles the upgrade row from the helper's exit code,
// then sweeps orphaned helper containers. The row MUST be reconciled before
// the sweep: the sweep removes the helper, and inspecting a removed helper
// would wrongly mark a successful upgrade as failed.
func (s *UpgradeService) ReconcileAtBoot(ctx context.Context) {
	if s.docker == nil {
		return
	}

	// Reconcile upgrade row FIRST (needs to inspect the helper container).
	row, err := s.repo.Get(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to get upgrade row during boot reconciliation", zap.Error(err))
		}
		return
	}
	if row == nil {
		return
	}

	if row.Status == "pending" || row.Status == "helper_running" {
		if row.HelperID != "" && s.docker != nil {
			inspected, err := s.docker.InspectContainer(ctx, row.HelperID)
			if err != nil || inspected == nil {
				// Helper is gone, mark as failed.
				if s.logger != nil {
					s.logger.Warn("upgrade helper container not found, marking upgrade as failed",
						zap.String("helper_id", row.HelperID),
					)
				}
				_ = s.repo.UpdateStatus(ctx, row.Status, "failed", row.HelperID, "helper container not found at boot")
				return
			}

			// Check exit code of helper.
			if inspected.Status == "exited" {
				if inspected.ExitCode == 0 {
					_ = s.repo.UpdateStatus(ctx, row.Status, "completed", row.HelperID, "")
				} else {
					_ = s.repo.UpdateStatus(ctx, row.Status, "failed", row.HelperID,
						fmt.Sprintf("helper exited with code %d", inspected.ExitCode))
				}
			}
			// If still running, leave as helper_running.
		} else {
			// No helper ID but pending -- mark as failed.
			_ = s.repo.UpdateStatus(ctx, row.Status, "failed", "", "no helper container launched")
		}
	}

	// Sweep orphan helpers AFTER row reconciliation (this removes the helper
	// the reconciliation just inspected, plus any from crashed attempts).
	helpers, err := s.docker.ListContainersByLabel(ctx, "io.composer.upgrade-helper=true")
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to list upgrade helper containers during boot reconciliation", zap.Error(err))
		}
		return
	}
	for _, h := range helpers {
		if s.logger != nil {
			s.logger.Info("removing orphaned upgrade helper container",
				zap.String("helper_id", h.ID),
			)
		}
		// Capture logs first, then remove (force if running).
		logs, logErr := s.captureContainerLogs(ctx, h.ID)
		if logErr != nil && s.logger != nil {
			s.logger.Warn("failed to capture helper logs", zap.String("helper_id", h.ID), zap.Error(logErr))
		}
		if err := s.docker.ContainerRemove(ctx, h.ID, true); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to remove orphan helper", zap.String("helper_id", h.ID), zap.Error(err))
			}
		}
		if s.logger != nil && logs != "" {
			s.logger.Info("orphan helper logs captured", zap.String("helper_id", h.ID), zap.String("logs", logs))
		}
	}
}

// captureContainerLogs reads the tail of a container's logs for diagnostics.
func (s *UpgradeService) captureContainerLogs(ctx context.Context, id string) (string, error) {
	if s.docker == nil {
		return "", nil
	}
	reader, err := s.docker.ContainerLogs(ctx, id, false /* follow */, "50" /* tail */, "")
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var buf [4096]byte
	n, err := reader.Read(buf[:])
	if err != nil && err.Error() != "EOF" {
		return "", err
	}
	return string(buf[:n]), nil
}

// ComposeInfo holds compose project details extracted from the running
// composer container's labels. Used by the helper to know where to run
// docker compose.
type ComposeInfo struct {
	ProjectName     string
	WorkingDir      string
	ConfigFile      string // colon-separated config files
	EnvironmentFile string
}
