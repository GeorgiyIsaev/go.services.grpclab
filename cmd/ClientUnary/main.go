package main

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "go.services.grpclab/gen/randompb"
)

func main() {
	// 1. Устанавливаем соединение с сервером.
	//    insecure.NewCredentials() — потому что у нас нет TLS,
	//    соединение идёт по plaintext (без шифрования).
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("не удалось подключиться: %v", err)
	}
	defer conn.Close()

	// 2. Создаём клиент — обёртку над соединением.
	client := pb.NewRandomClient(conn)

	// Первый вызов
	callGetRandom(client)

	// Второй вызов
	time.Sleep(10 * time.Second) //через 10 сек
	callGetRandom(client)

	//По одному вызову по одному ответу
}

// callGetRandom делает один Unary-вызов и печатает результат.
func callGetRandom(client pb.RandomClient) {
	// Каждый вызов оборачиваем в context с таймаутом.
	// Если сервер не ответит за 5 секунд — вызов прервётся.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetRandom(ctx, &pb.GetRandomRequest{})
	if err != nil {
		log.Printf("ошибка вызова GetRandom: %v", err)
		return
	}

	log.Printf("случайное число: %d", resp.GetValue())
}
