package services

import (
	"context"
	"errors"

	"github.com/Pelfox/codecell-runner/internal/executor"
	"github.com/docker/go-units"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// imagesMapping maps supported programming languages to their corresponding executor technologies.
var imagesMapping = map[string]executor.Technology{
	"dotnet": executor.DotNetTechnology{},
}

// ContainersService provides methods to manage Docker containers for code execution.
type ContainersService struct {
	dockerClient *client.Client
}

// NewContainersService creates a new instance of ContainersService with the given Docker client.
func NewContainersService(dockerClient *client.Client) *ContainersService {
	return &ContainersService{dockerClient}
}

// CreateContainer creates a new container for the given language and source code.
// It returns the container ID or an error if the operation fails.
func (s *ContainersService) CreateContainer(language string, sourceCode string) (string, error) {
	technology, ok := imagesMapping[language]
	if !ok {
		return "", errors.New("the specified language is not supported")
	}

	initValue := true      // enabling init process in the container
	pidsLimit := int64(64) // limiting the number of processes to 64
	containerOptions := client.ContainerCreateOptions{
		Config: &container.Config{
			User:         "runner", // running as non-root
			AttachStdout: true,
			AttachStderr: true,
			Env: []string{
				"HOME=/tmp",
				"TZ=Europe/Moscow",
			},
			Cmd:        technology.GetCommand(),
			WorkingDir: "/workspace",
			Volumes: map[string]struct{}{
				"/workspace": {},
			},
			NetworkDisabled: true,
			// opening and attaching STDIN, to write input from the user
			OpenStdin:   true,
			StdinOnce:   true,
			AttachStdin: true,
		},
		HostConfig: &container.HostConfig{
			IpcMode:        "none",
			Init:           &initValue,
			ReadonlyRootfs: true, // making root filesystem read-only
			Tmpfs: map[string]string{
				"/tmp": "rw,noexec,nosuid,size=64m",
			},
			NetworkMode: "none", // TODO: disable network to prevent attacks
			AutoRemove:  true,
			CapDrop:     []string{"ALL"}, // dropping all capabilities for security
			SecurityOpt: []string{
				"no-new-privileges", // preventing privilege escalation
			},
			MaskedPaths: []string{
				"/proc/acpi",
				"/proc/kcore",
				"/proc/keys",
				"/proc/latency_stats",
				"/proc/timer_list",
				"/proc/timer_stats",
				"/proc/sched_debug",
				"/proc/scsi",
				"/sys/firmware",
				"/sys/kernel/debug",
				"/sys/kernel/tracing",
			},
			ReadonlyPaths: []string{
				"/proc/asound",
				"/proc/bus",
				"/proc/fs",
				"/proc/irq",
				"/proc/sys",
				"/proc/sysrq-trigger",
			},
			Resources: container.Resources{
				Memory:     512 * 1024 * 1024, // limit memory to 512MB
				MemorySwap: 512 * 1024 * 1024, // disable swap
				NanoCPUs:   1_000_000_000,     // allow only 1 CPU
				PidsLimit:  &pidsLimit,
				Ulimits: []*units.Ulimit{
					{Name: "nofile", Soft: 1024, Hard: 1024},
					{Name: "fsize", Soft: 100 * 1024 * 1024, Hard: 100 * 1024 * 1024}, // Limit file size to 100MB
				},
			},
			StorageOpt: map[string]string{
				"size": "512M",
			},
			// TODO: use "Kata Containers" or "gVisor" for better isolation
		},
		Image: technology.GetImage(),
	}

	result, err := s.dockerClient.ContainerCreate(context.Background(), containerOptions)
	if err != nil {
		return "", err
	}

	workspaceReader, err := technology.WriteSourceCode(sourceCode)
	if err != nil {
		return "", err
	}

	copyOptions := client.CopyToContainerOptions{
		DestinationPath: "/workspace",
		Content:         workspaceReader,
	}
	_, err = s.dockerClient.CopyToContainer(context.Background(), result.ID, copyOptions)

	return result.ID, err
}

// StartContainer start the container with the given ID. It must be run after
// the container is created, and after LogsService is attached to it.
func (s *ContainersService) StartContainer(containerID string) error {
	_, err := s.dockerClient.ContainerStart(context.Background(), containerID, client.ContainerStartOptions{})
	return err
}

// WaitForContainer waits for the container with the given ID to stop running.
// It returns two channels: one for the wait response and another for errors.
func (s *ContainersService) WaitForContainer(
	ctx context.Context,
	containerID string,
) (<-chan container.WaitResponse, <-chan error) {
	options := client.ContainerWaitOptions{
		// waiting till the container stops running
		Condition: container.WaitConditionNotRunning,
	}
	result := s.dockerClient.ContainerWait(ctx, containerID, options)
	return result.Result, result.Error
}

// KillContainer forcefully kills the container with the given ID using SIGKILL signal.
func (s *ContainersService) KillContainer(containerID string) error {
	options := client.ContainerKillOptions{
		Signal: "SIGKILL",
	}
	_, err := s.dockerClient.ContainerKill(context.Background(), containerID, options)
	return err
}

// RemoveContainer removes the container with the given ID from the Docker host.
func (s *ContainersService) RemoveContainer(containerID string) error {
	_, err := s.dockerClient.ContainerRemove(context.Background(), containerID, client.ContainerRemoveOptions{})
	return err
}
