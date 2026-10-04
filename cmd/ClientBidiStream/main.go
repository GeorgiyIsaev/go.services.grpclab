package main

import (
	"context"
	"errors"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync"
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

	// 2. Контекст, отменяемый по Ctrl+C.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("получен сигнал, закрываем поток...")
		cancel()
	}()

	// 3. Открываем двунаправленный поток.
	//    Обратите внимание: нет ни req, ни возврата — общаемся через Send/Recv.
	stream, err := client.CompareNumbers(ctx)
	if err != nil {
		log.Fatalf("не удалось открыть поток: %v", err)
	}

	log.Println("поток открыт, начинаем обмен...")

	// 4. Отправку выносим в отдельную горутину.
	//    Send и Recv — блокирующие, поэтому для одновременного
	//    чтения и записи нужны две горутины.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		total := 10
		for i := 1; i <= total; i++ {
			v := int32(rand.Intn(9) + 1)

			if err := stream.Send(&pb.CompareNumbersRequest{Value: v}); err != nil {
				log.Printf("ошибка отправки #%d: %v", i, err)
				return
			}
			log.Printf("отправлено #%d: %d", i, v)

			time.Sleep(500 * time.Millisecond)
		}

		// Закрываем свою сторону потока: сообщаем серверу,
		// что новых сообщений не будет. Чтение при этом продолжается.
		if err := stream.CloseSend(); err != nil {
			log.Printf("ошибка CloseSend: %v", err)
		}
		log.Println("отправка завершена, ждём последние ответы...")
	}()

	// 5. Чтение — в основной горутине.
	for {
		resp, err := stream.Recv()

		if errors.Is(err, io.EOF) {
			log.Println("сервер закрыл поток")
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				log.Println("поток закрыт клиентом")
				break
			}
			log.Printf("ошибка приёма: %v", err)
			break
		}

		log.Printf("ответ: client=%d server=%d → %s",
			resp.GetClientValue(),
			resp.GetServerValue(),
			resp.GetResult(),
		)
	}

	wg.Wait()
	log.Println("завершено")
}
