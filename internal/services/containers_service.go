package services

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Pelfox/codecell-runner/internal/executor"
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

	// creating a temporary directory for the container workspace
	workspaceDir, err := os.MkdirTemp("", "codecell-workspace-")
	if err != nil {
		return "", errors.New("failed to create temporary workspace directory")
	}

	// synchronously applying all technology's steps
	for _, step := range technology.GetSteps() {
		if err := step(workspaceDir, sourceCode); err != nil {
			return "", err
		}
	}

	containerOptions := client.ContainerCreateOptions{
		Config: &container.Config{
			User:         "runner", // running as non-root
			AttachStdout: true,
			AttachStderr: true,
			// Env:             nil, // TODO: fill environment variables, if needed
			Cmd:        technology.GetCommand(),
			WorkingDir: "/workspace",

			// opening and attaching STDIN, to write input from the user
			OpenStdin:   true,
			StdinOnce:   true,
			AttachStdin: true,
		},
		HostConfig: &container.HostConfig{
			Binds: []string{
				fmt.Sprintf("%s:/workspace:rw", workspaceDir), // TODO: mount a temporary FS and switch to RO
			},
			// NetworkMode: "none", // TODO: disable network to prevent attacks
			AutoRemove: false,
		},
		Image: technology.GetImage(),
	}

	result, err := s.dockerClient.ContainerCreate(context.Background(), containerOptions)
	if err != nil {
		return "", err
	}

	return result.ID, nil
}

// StartContainer start the container with the given ID. It must be run after
// the container is created, and after LogsService is attached to it.
func (s *ContainersService) StartContainer(containerID string) error {
	// TODO: killing for a long-running containers
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
