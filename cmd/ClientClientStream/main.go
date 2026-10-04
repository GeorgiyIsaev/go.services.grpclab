package main

import (
	"context"
	"log"
	"math/rand"
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

	// 2. Контекст с общим дедлайном на весь вызов.
	//    Если весь сеанс займёт больше 30 секунд — прервём.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 3. Открываем поток на отправку. Обратите внимание: возврата
	//    из этого вызова нет — только объект-стрим и ошибка.
	stream, err := client.UploadNumbers(ctx)
	if err != nil {
		log.Fatalf("не удалось открыть поток: %v", err)
	}

	// 4. Отправляем 10 случайных чисел с паузой 500 мс.
	total := 10
	log.Printf("начинаем отправку %d чисел...", total)
	for i := 1; i <= total; i++ {
		v := int32(rand.Intn(9) + 1)

		if err := stream.Send(&pb.UploadNumbersRequest{Value: v}); err != nil {
			log.Fatalf("ошибка отправки #%d: %v", i, err)
		}
		log.Printf("отправлено #%d: %d", i, v)

		time.Sleep(500 * time.Millisecond)
	}

	// 5. Сообщаем серверу, что отправка закончена, и получаем итог.
	//    CloseAndRecv закрывает нашу сторону потока и ждёт одно сообщение ответа.
	resp, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("ошибка получения итога: %v", err)
	}

	log.Println("--- итог от сервера ---")
	log.Printf("количество: %d", resp.GetCount())
	log.Printf("сумма:      %d", resp.GetSum())
	log.Printf("среднее:    %.2f", resp.GetAverage())
	log.Printf("минимум:    %d", resp.GetMin())
	log.Printf("максимум:   %d", resp.GetMax())
}
