package main

import (
	"context"
	"log"
	"math/rand"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "go.services.grpclab/gen/randompb"
)

// server реализует интерфейс pb.RandomServer.
type server struct {
	pb.UnimplementedRandomServer // обязательно встраивать для совместимости
}

// GetRandom — наша единственная ручка.
func (s *server) GetRandom(ctx context.Context, _ *pb.GetRandomRequest) (*pb.GetRandomResponse, error) {
	n := rand.Intn(9) + 1 // rand.Intn(9) даёт 0..8, +1 → 1..9
	return &pb.GetRandomResponse{Value: int32(n)}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("не удалось занять порт: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterRandomServer(s, &server{})

	// Reflection позволит тестировать сервер через grpcurl без .proto файла.
	reflection.Register(s)

	log.Println("gRPC-сервер слушает :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("ошибка сервера: %v", err)
	}
}
