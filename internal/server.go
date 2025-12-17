package internal

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	v1 "github.com/Pelfox/codecell-runner/generated"
	"github.com/Pelfox/codecell-runner/internal/services"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RunnerServer implements the gRPC server for the runner service protocol definition.
type RunnerServer struct {
	v1.UnimplementedRunnerServiceServer

	containersService *services.ContainersService
	logsService       *services.LogsService

	mutex    sync.Mutex
	requests map[string]string // ID = request ID, Value = container ID
	cancels  map[string]context.CancelFunc
}

// NewRunnerServer creates a new instance of RunnerServer with the given subservices.
func NewRunnerServer(containersService *services.ContainersService, logsService *services.LogsService) *RunnerServer {
	return &RunnerServer{
		containersService: containersService,
		logsService:       logsService,

		mutex:    sync.Mutex{},
		requests: make(map[string]string),
		cancels:  make(map[string]context.CancelFunc),
	}
}

func (s *RunnerServer) Run(request *v1.RunRequest, stream grpc.ServerStreamingServer[v1.RunResponseMessage]) error {
	requestID := uuid.New()
	// top-level function for writing messages with the string (human-readable) payload
	writeMessage := func(level v1.MessageLevel, message string) error {
		err := stream.Send(&v1.RunResponseMessage{
			RequestId: requestID.String(),
			Level:     level,
			Payload:   &v1.RunResponseMessage_Message{Message: message},
		})
		if err != nil {
			log.Error().Str("requestID", requestID.String()).
				Err(err).
				Msg("failed to send message to the stream")
			return err
		}
		return nil
	}

	timeout := time.Duration(request.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(stream.Context(), timeout)
	defer cancel()

	if err := writeMessage(v1.MessageLevel_INFO, "Starting up container..."); err != nil {
		return err
	}
	log.Info().Str("requestID", requestID.String()).Msg("starting up container for request")

	defer func() {
		s.mutex.Lock()
		containerID, ok := s.requests[requestID.String()]
		s.mutex.Unlock()

		if ok {
			_ = s.containersService.RemoveContainer(containerID)
			log.Info().Str("requestID", requestID.String()).
				Str("containerID", containerID).
				Msg("container removed after request completion")
		}

		s.mutex.Lock()
		delete(s.requests, requestID.String())
		delete(s.cancels, requestID.String())
		s.mutex.Unlock()
	}()

	// creating the container for the request
	containerID, err := s.containersService.CreateContainer(requestID.String(), request.Language, request.SourceCode)
	if err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to create the container")
		return writeMessage(v1.MessageLevel_ERROR, fmt.Sprintf("Failed to create container: %v", err))
	}

	// storing the container ID, as well as cancel function for timeout handling
	s.mutex.Lock()
	s.requests[requestID.String()] = containerID
	s.cancels[requestID.String()] = cancel
	s.mutex.Unlock()

	if err := writeMessage(v1.MessageLevel_INFO, "Execution container is created."); err != nil {
		return err
	}

	// enabling the streaming of the logs for the container
	stdin, stdoutChannel, stderrChannel, err := s.logsService.AttachIO(ctx, containerID)
	if err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to attach to the container logs")
		return writeMessage(v1.MessageLevel_ERROR, "Failed to attach to the container.")
	}

	// starting the container execution
	if err := s.containersService.StartContainer(containerID); err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to start the container")
		return writeMessage(v1.MessageLevel_ERROR, "Failed to start the container.")
	}

	// writing all provided STDIN request lines to the container
	for _, line := range request.Stdin {
		if _, err = io.WriteString(stdin, line+"\n"); err != nil {
			log.Error().Str("requestID", requestID.String()).
				Err(err).
				Msg("failed to write to the container stdin")
			return writeMessage(v1.MessageLevel_ERROR, "Failed to write to the container stdin.")
		}
	}

	// try to close the stdin; hack is to close only the write part of the connection
	if closer, ok := stdin.(interface{ CloseWrite() error }); ok {
		if err := closer.CloseWrite(); err != nil {
			log.Error().Str("requestID", requestID.String()).
				Err(err).
				Msg("failed to close the container stdin")
		}
	}

	// getting container statistics stream
	statisticsChannel, err := s.containersService.StreamContainerStatistics(ctx, containerID)
	if err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to stream container statistics")
		return writeMessage(v1.MessageLevel_ERROR, "Failed to stream container statistics.")
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			case stats, ok := <-statisticsChannel:
				if !ok {
					return
				}

				// calculate usage of the CPU
				cpuDelta := float32(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
				systemDelta := float32(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
				cpuUsagePercent := (cpuDelta / systemDelta) * float32(stats.CPUStats.OnlineCPUs) * 100.0

				if err := stream.Send(&v1.RunResponseMessage{
					RequestId: requestID.String(),
					Level:     v1.MessageLevel_STATISTICS,
					Payload: &v1.RunResponseMessage_Statistics{
						Statistics: &v1.StatisticsMessage{
							MemoryUsed: stats.MemoryStats.Usage,
							CpuPercent: cpuUsagePercent,
						},
					},
				}); err != nil {
					log.Error().Str("requestID", requestID.String()).
						Err(err).
						Msg("failed to send statistics to the stream")
				}
			}
		}
	}()

	// FIXME: allow only up to 100 KB of logs to be sent back to the client

	// waiting for the container to finish execution
	statusChannel, errorChannel := s.containersService.WaitForContainer(ctx, containerID)
	for stdoutChannel != nil || stderrChannel != nil || statusChannel != nil {
		select {
		// if the container has timed out, kill it and notify the client
		case <-ctx.Done():
			if err := s.containersService.KillContainer(containerID); err != nil {
				log.Error().Str("requestID", requestID.String()).
					Str("containerID", containerID).
					Err(err).
					Msg("failed to kill the container on timeout")
			}
			if err := writeMessage(v1.MessageLevel_ERROR, "Execution timed out."); err != nil {
				return err
			}
			return ctx.Err()

		// relay all logs from the stdout channel
		case msg, ok := <-stdoutChannel:
			if !ok {
				stdoutChannel = nil
				continue
			}
			if err := writeMessage(v1.MessageLevel_STDOUT, msg); err != nil {
				return err
			}

		// relay all logs from the stderr channel
		case msg, ok := <-stderrChannel:
			if !ok {
				stderrChannel = nil
				continue
			}
			if err := writeMessage(v1.MessageLevel_STDERR, msg); err != nil {
				return err
			}

		// handle container execution errors
		case err := <-errorChannel:
			if err != nil {
				if err := writeMessage(v1.MessageLevel_ERROR, err.Error()); err != nil {
					return err
				}
				return err
			}

		// handle container exit status
		case exitStatus := <-statusChannel:
			if err := stream.Send(&v1.RunResponseMessage{
				RequestId: requestID.String(),
				Level:     v1.MessageLevel_EXIT_CODE,
				Payload:   &v1.RunResponseMessage_ExitCode{ExitCode: exitStatus.StatusCode},
			}); err != nil {
				log.Error().Str("requestID", requestID.String()).
					Err(err).
					Msg("failed to send exit code to the stream")
				return err
			}
			statusChannel = nil
			errorChannel = nil
		}
	}

	return nil
}

func (s *RunnerServer) Stop(_ context.Context, request *v1.StopRequest) (*v1.StopResponse, error) {
	s.mutex.Lock()
	containerID, containerOk := s.requests[request.RequestId]
	cancel, cancelOk := s.cancels[request.RequestId]
	s.mutex.Unlock()

	if !containerOk {
		return nil, status.Errorf(codes.NotFound, "container not found")
	}

	// killing the container if request requires force stop
	if request.Force {
		if err := s.containersService.KillContainer(containerID); err != nil {
			log.Info().Str("requestID", request.RequestId).
				Str("containerID", containerID).
				Err(err).
				Msg("failed to kill the container on force stop request")
			return nil, status.Errorf(codes.Internal, "failed to kill the container: %v", err)
		}
		log.Info().Str("requestID", request.RequestId).
			Str("containerID", containerID).
			Msg("container killed on force stop request")
		return &v1.StopResponse{}, nil
	}

	// cancelling the execution, `Run` function will handle this by itself
	if cancelOk {
		cancel()
	}

	log.Info().Str("requestID", request.RequestId).
		Str("containerID", containerID).
		Msg("container stopped on stop request")
	return &v1.StopResponse{}, nil
}
