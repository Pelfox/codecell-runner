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
	timeout := time.Duration(request.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(stream.Context(), timeout)
	defer cancel()

	requestID := uuid.New()
	if err := stream.Send(&v1.RunResponseMessage{
		RequestId: requestID.String(),
		Level:     v1.MessageLevel_INFO,
		Message:   "Starting up the container...",
	}); err != nil {
		return err
	}
	log.Info().Str("requestID", requestID.String()).
		Interface("request", request).
		Msg("starting up the container for request")

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
	containerID, err := s.containersService.CreateContainer(request.Language, request.SourceCode)
	if err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to create the container")
		return stream.Send(&v1.RunResponseMessage{
			RequestId: requestID.String(),
			Level:     v1.MessageLevel_ERROR,
			Message:   "Failed to create the container: " + err.Error(),
		})
	}

	// storing the container ID, as well as cancel function for timeout handling
	s.mutex.Lock()
	s.requests[requestID.String()] = containerID
	s.cancels[requestID.String()] = cancel
	s.mutex.Unlock()

	if err := stream.Send(&v1.RunResponseMessage{
		RequestId: requestID.String(),
		Level:     v1.MessageLevel_INFO,
		Message:   "Runner container is created",
	}); err != nil {
		return err
	}

	// enabling the streaming of the logs for the container
	stdin, stdoutChannel, stderrChannel, err := s.logsService.AttachIO(ctx, containerID)
	if err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to attach to the container logs")
		return stream.Send(&v1.RunResponseMessage{
			RequestId: requestID.String(),
			Level:     v1.MessageLevel_ERROR,
			Message:   "Failed to attach to the container logs: " + err.Error(),
		})
	}

	// starting the container execution
	if err := s.containersService.StartContainer(containerID); err != nil {
		log.Error().Str("requestID", requestID.String()).
			Err(err).
			Msg("failed to start the container")
		return stream.Send(&v1.RunResponseMessage{
			RequestId: requestID.String(),
			Level:     v1.MessageLevel_ERROR,
			Message:   "Failed to start the container: " + err.Error(),
		})
	}

	// writing all provided STDIN request lines to the container
	for _, line := range request.Stdin {
		if _, err = io.WriteString(stdin, line+"\n"); err != nil {
			log.Error().Str("requestID", requestID.String()).
				Err(err).
				Msg("failed to write to the container stdin")
			return stream.Send(&v1.RunResponseMessage{
				RequestId: requestID.String(),
				Level:     v1.MessageLevel_ERROR,
				Message:   "Failed to write to the container stdin: " + err.Error(),
			})
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

	// FIXME: allow only up to 100 KB of logs to be sent back to the client
	// FIXME: don't ignore `error` on stream.Send.

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
			_ = stream.Send(&v1.RunResponseMessage{
				RequestId: requestID.String(),
				Level:     v1.MessageLevel_ERROR,
				Message:   "Execution timed out",
			})
			return ctx.Err()

		// relay all logs from the stdout channel
		case msg, ok := <-stdoutChannel:
			if !ok {
				stdoutChannel = nil
				continue
			}
			stream.Send(&v1.RunResponseMessage{
				RequestId: requestID.String(),
				Level:     v1.MessageLevel_STDOUT,
				Message:   msg,
			})

		// relay all logs from the stderr channel
		case msg, ok := <-stderrChannel:
			if !ok {
				stderrChannel = nil
				continue
			}
			stream.Send(&v1.RunResponseMessage{
				RequestId: requestID.String(),
				Level:     v1.MessageLevel_STDERR,
				Message:   msg,
			})

		// handle container execution errors
		case err := <-errorChannel:
			if err != nil {
				stream.Send(&v1.RunResponseMessage{
					RequestId: requestID.String(),
					Level:     v1.MessageLevel_ERROR,
					Message:   err.Error(),
				})
				return err
			}

		// handle container exit status
		case exitStatus := <-statusChannel:
			stream.Send(&v1.RunResponseMessage{
				RequestId: requestID.String(),
				Level:     v1.MessageLevel_EXIT_CODE,
				Message:   fmt.Sprintf("Container has exited with code %d", exitStatus.StatusCode),
			})
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
