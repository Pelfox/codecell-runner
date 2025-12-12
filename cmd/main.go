package main

import (
	"net"

	v1 "github.com/Pelfox/codecell-runner/generated"
	"github.com/Pelfox/codecell-runner/internal"
	"github.com/Pelfox/codecell-runner/internal/services"
	"github.com/moby/moby/client"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func main() {
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create docker client")
	}
	defer dockerClient.Close()

	containerService := services.NewContainersService(dockerClient)
	logsService := services.NewLogsService(dockerClient)
	server := internal.NewRunnerServer(containerService, logsService)

	grpcServer := grpc.NewServer()
	v1.RegisterRunnerServiceServer(grpcServer, server)

	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen")
	}
	defer listener.Close()

	log.Info().Int("port", 50051).Msg("gRPC server listening")
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatal().Err(err).Msg("failed to serve gRPC")
	}
}
