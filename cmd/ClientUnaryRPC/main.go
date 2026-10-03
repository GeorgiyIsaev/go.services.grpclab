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

	// 3. Раз в 30 секунд дёргаем GetRandom и печатаем ответ.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Сразу дёрнем один раз, не дожидаясь первых 30 секунд.
	callGetRandom(client)

	for range ticker.C {
		callGetRandom(client)
	}
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
