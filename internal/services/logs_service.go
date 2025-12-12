package services

import (
	"bufio"
	"context"
	"io"
	"sync"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// scanLines reads lines from the given reader and sends them to the output channel.
func scanLines(r io.Reader, out chan<- string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // setting buffer size to 1MB

	for scanner.Scan() {
		out <- scanner.Text()
	}
}

// LogsService provides methods to stream logs from Docker containers.
type LogsService struct {
	dockerClient *client.Client
}

// NewLogsService creates a new instance of LogsService with the given Docker client.
func NewLogsService(dockerClient *client.Client) *LogsService {
	return &LogsService{dockerClient}
}

// AttachIO streams the stdout and stderr logs of the specified container, as well
// as opens the STDIN writer.
func (s *LogsService) AttachIO(
	ctx context.Context,
	containerID string,
) (
	stdin io.WriteCloser,
	stdout <-chan string,
	stderr <-chan string,
	err error,
) {
	outCh := make(chan string)
	errCh := make(chan string)

	resp, err := s.dockerClient.ContainerAttach(
		ctx,
		containerID,
		client.ContainerAttachOptions{
			Stream: true,
			Stdin:  true,
			Stdout: true,
			Stderr: true,
			Logs:   true,
		},
	)
	if err != nil {
		return nil, nil, nil, err
	}

	// reading STDIN from the hijacked connection to the container
	stdin = resp.Conn

	go func() {
		defer close(outCh)
		defer close(errCh)
		defer resp.Close()

		stdoutR, stdoutW := io.Pipe()
		stderrR, stderrW := io.Pipe()

		var wg sync.WaitGroup
		wg.Add(2)

		// STDOUT scanner
		go func() {
			defer wg.Done()
			scanLines(stdoutR, outCh)
		}()

		// STDERR scanner
		go func() {
			defer wg.Done()
			scanLines(stderrR, errCh)
		}()

		// Demultiplex Docker stream
		go func() {
			defer stdoutW.Close()
			defer stderrW.Close()
			_, _ = stdcopy.StdCopy(stdoutW, stderrW, resp.Reader)
		}()

		wg.Wait()
	}()

	return stdin, outCh, errCh, nil
}
