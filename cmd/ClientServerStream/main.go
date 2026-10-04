package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "go.services.grpclab/gen/randompb"
)

func main() {
	// 1. Соединение с сервером.
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("не удалось подключиться: %v", err)
	}
	defer conn.Close()

	client := pb.NewRandomClient(conn)

	// 2. Контекст, который можно отменить по Ctrl+C.
	//    Отмена контекста корректно закроет поток и на стороне сервера.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ловим Ctrl+C и отменяем контекст.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("получен сигнал, закрываем поток...")
		cancel()
	}()

	// 3. Один запрос — открываем поток.
	//    interval_seconds=2 — для наглядности, count=5 — получим ровно 5 чисел.
	req := &pb.StreamRandomRequest{
		IntervalSeconds: 2,
		Count:           5,
	}
	log.Printf("отправляем запрос: interval=%ds, count=%d", req.IntervalSeconds, req.Count)

	stream, err := client.StreamRandom(ctx, req)
	if err != nil {
		log.Fatalf("не удалось открыть поток: %v", err)
	}

	// 4. Читаем поток, пока сервер не закроет его.
	log.Println("ждём сообщения от сервера...")
	for {
		resp, err := stream.Recv()

		// io.EOF — сервер корректно закрыл поток. Это не ошибка.
		if errors.Is(err, io.EOF) {
			log.Println("сервер закрыл поток")
			return
		}
		if err != nil {
			// Если мы сами отменили контекст (Ctrl+C) — это не ошибка.
			if ctx.Err() != nil {
				log.Println("поток закрыт клиентом")
				return
			}
			log.Fatalf("ошибка приёма: %v", err)
		}

		log.Printf("получено: значение=%d, время=%s",
			resp.GetValue(),
			time.Unix(resp.GetTimestampUnix(), 0).Format(time.TimeOnly),
		)
	}
}
