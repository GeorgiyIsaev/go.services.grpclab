package main

import (
	"context"
	"errors"
	"io"
	"log"
	"math/rand"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "go.services.grpclab/gen/randompb"
)

type server struct {
	pb.UnimplementedRandomServer
}

// ---------- 1. Unary ----------

func (s *server) GetRandom(_ context.Context, _ *pb.GetRandomRequest) (*pb.GetRandomResponse, error) {
	n := randInt()
	log.Printf("[Unary] GetRandom → %d", n)
	return &pb.GetRandomResponse{Value: n}, nil
}

// ---------- 2. Server Streaming ----------

func (s *server) StreamRandom(req *pb.StreamRandomRequest, stream pb.Random_StreamRandomServer) error {
	interval := time.Duration(req.GetIntervalSeconds()) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	limit := req.GetCount()

	log.Printf("[ServerStream] клиент подписался: интервал=%s, count=%d", interval, limit)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent int32
	for {
		select {
		case <-stream.Context().Done():
			log.Println("[ServerStream] клиент отключился")
			return stream.Context().Err()

		case <-ticker.C:
			resp := &pb.StreamRandomResponse{
				Value:         randInt(),
				TimestampUnix: time.Now().Unix(),
			}
			if err := stream.Send(resp); err != nil {
				log.Printf("[ServerStream] ошибка отправки: %v", err)
				return err
			}
			sent++
			log.Printf("[ServerStream] отправлено #%d: %d", sent, resp.GetValue())

			if limit > 0 && sent >= limit {
				log.Println("[ServerStream] лимит достигнут, закрываем поток")
				return nil
			}
		}
	}
}

// ---------- 3. Client Streaming ----------

func (s *server) UploadNumbers(stream pb.Random_UploadNumbersServer) error {
	log.Println("[ClientStream] клиент начал отправку")

	var (
		count int32
		sum   int64
		min   int32
		max   int32
		first = true
	)

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// Клиент закончил отправку — отвечаем одним сообщением.
			if count == 0 {
				log.Println("[ClientStream] клиент не прислал ни одного числа")
				return stream.SendAndClose(&pb.UploadNumbersResponse{})
			}
			resp := &pb.UploadNumbersResponse{
				Count:   count,
				Sum:     sum,
				Average: float64(sum) / float64(count),
				Min:     min,
				Max:     max,
			}
			log.Printf("[ClientStream] итог: count=%d sum=%d avg=%.2f min=%d max=%d",
				resp.Count, resp.Sum, resp.Average, resp.Min, resp.Max)
			return stream.SendAndClose(resp)
		}
		if err != nil {
			log.Printf("[ClientStream] ошибка приёма: %v", err)
			return err
		}

		v := req.GetValue()
		count++
		sum += int64(v)
		if first {
			min, max, first = v, v, false
		} else {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		log.Printf("[ClientStream] принято #%d: %d", count, v)
	}
}

// ---------- 4. Bidirectional Streaming ----------

func (s *server) CompareNumbers(stream pb.Random_CompareNumbersServer) error {
	log.Println("[Bidi] клиент подключился")

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			log.Println("[Bidi] клиент закрыл поток")
			return nil
		}
		if err != nil {
			log.Printf("[Bidi] ошибка приёма: %v", err)
			return err
		}

		clientValue := req.GetValue()
		serverValue := randInt()

		result := "equal"
		switch {
		case clientValue > serverValue:
			result = "client bigger"
		case clientValue < serverValue:
			result = "server bigger"
		}

		resp := &pb.CompareNumbersResponse{
			ClientValue: clientValue,
			ServerValue: serverValue,
			Result:      result,
		}
		if err := stream.Send(resp); err != nil {
			log.Printf("[Bidi] ошибка отправки: %v", err)
			return err
		}
		log.Printf("[Bidi] client=%d server=%d → %s", clientValue, serverValue, result)
	}
}

// randInt возвращает случайное число 1..9.
func randInt() int32 {
	return int32(rand.Intn(9) + 1)
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("не удалось занять порт: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterRandomServer(s, &server{})
	reflection.Register(s)

	log.Println("gRPC-сервер Random слушает :50051")
	log.Println("методы: GetRandom (Unary), StreamRandom (ServerStream), UploadNumbers (ClientStream), CompareNumbers (Bidi)")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("ошибка сервера: %v", err)
	}
}
