[Главный](Readme.md)

# ServerRandom

Единственный серверный бинарник в проекте. Реализует **все четыре** метода
сервиса `Random`, описанного в `proto/random.proto`:

- `GetRandom` — Unary RPC
- `StreamRandom` — Server Streaming RPC
- `UploadNumbers` — Client Streaming RPC
- `CompareNumbers` — Bidirectional Streaming RPC

Это самый содержательный файл проекта: здесь видно, как один `.proto`
превращается в четыре метода с принципиально разными сигнатурами, и как
`main` собирает всё вместе.

---

## 🎯 Что делает сервер

1. Слушает `:50051`.
2. Регистрирует на gRPC-сервере реализацию `RandomServer`.
3. Включает **reflection** — чтобы `grpcurl` и `grpcui` могли исследовать
   сервер без `.proto`-файла.
4. Обслуживает запросы от всех четырёх клиентов одновременно.

Сервер stateless: он ничего не хранит между вызовами. Каждый вызов получает
свои локальные переменные, свою горутину и свой контекст.

---

## 📚 Контракт глазами сервера

### Один `service` — четыре `rpc`

`random.proto` описывает **один сервис** с четырьмя методами:

```protobuf
service Random {
  rpc GetRandom(GetRandomRequest) returns (GetRandomResponse);
  rpc StreamRandom(StreamRandomRequest) returns (stream StreamRandomResponse);
  rpc UploadNumbers(stream UploadNumbersRequest) returns (UploadNumbersResponse);
  rpc CompareNumbers(stream CompareNumbersRequest) returns (stream CompareNumbersResponse);
}
```

`protoc` читает эти четыре строки и генерирует:

- **Интерфейс `RandomServer`** — то, что должен реализовать сервер.
- **Четыре обработчика** — `_Random_GetRandom_Handler` и т.д.
- **`Random_ServiceDesc`** — таблицу маршрутизации: «имя метода → обработчик».

Ключевое наблюдение: **разница между паттернами видна прямо в `.proto`**
по расположению ключевого слова `stream`:

| Где стоит `stream` | Что это значит | Паттерн |
|---|---|---|
| Нигде | Клиент 1 → сервер 1 | Unary |
| Справа от `returns` | Клиент 1 → сервер поток | Server Streaming |
| Слева от `returns` | Клиент поток → сервер 1 | Client Streaming |
| С обеих сторон | Клиент поток ↔ сервер поток | Bidirectional |

### Как `rpc` превращается в Go-сигнатуру

`protoc-gen-go-grpc` смотрит на каждую строку `rpc` и генерирует метод в
интерфейсе `RandomServer`. Правило преобразования:

| Что в `.proto` | Что в Go-сигнатуре |
|---|---|
| Нет `stream` нигде | `func(ctx, *Req) (*Resp, error)` |
| `stream` справа | `func(*Req, StreamServer) error` |
| `stream` слева | `func(StreamServer) error` |
| `stream` с обеих сторон | `func(StreamServer) error` |

Применим к нашему `.proto`:

```protobuf
rpc GetRandom(GetRandomRequest) returns (GetRandomResponse);
```
↓
```go
func (s *server) GetRandom(ctx context.Context, req *pb.GetRandomRequest) (*pb.GetRandomResponse, error)
```

```protobuf
rpc StreamRandom(StreamRandomRequest) returns (stream StreamRandomResponse);
```
↓
```go
func (s *server) StreamRandom(req *pb.StreamRandomRequest, stream pb.Random_StreamRandomServer) error
```

```protobuf
rpc UploadNumbers(stream UploadNumbersRequest) returns (UploadNumbersResponse);
```
↓
```go
func (s *server) UploadNumbers(stream pb.Random_UploadNumbersServer) error
```

```protobuf
rpc CompareNumbers(stream CompareNumbersRequest) returns (stream CompareNumbersResponse);
```
↓
```go
func (s *server) CompareNumbers(stream pb.Random_CompareNumbersServer) error
```

**Правило, которое стоит запомнить:**

- Есть `stream` справа (сервер шлёт поток) → во втором аргументе появляется `stream pb.<Service>_<Method>Server`, а `return` — только `error`.
- Есть `stream` слева (клиент шлёт поток) → **нет аргумента `req`**, потому что запросов много. Ответ уходит через `stream.SendAndClose` (для Client Streaming) или `stream.Send` (для Bidi).
- Нет `stream` нигде (Unary) → обычный `(ctx, req) → (resp, error)`.

### Имя метода в сети

Помимо сигнатур, `protoc` запоминает **полное имя метода**, собранное из
`package`, имени сервиса и имени метода:

```
/random.Random/GetRandom
/random.Random/StreamRandom
/random.Random/UploadNumbers
/random.Random/CompareNumbers
```

Именно эти строки клиент кладёт в HTTP/2-заголовок `:path`, и по ним
gRPC-сервер находит нужный обработчик. Если изменить `package` в `.proto`
— изменится путь, и старые клиенты перестанут попадать в метод.

### Что генерируется помимо интерфейса

`random_grpc.pb.go` содержит:

1. **Интерфейс `RandomServer`** — контракт для реализации.
2. **`UnimplementedRandomServer`** — готовая заглушка, которую мы встраиваем.
3. **`Random_ServiceDesc`** — таблица маршрутизации.
4. **Четыре обработчика** — по одному на метод.
5. **Четыре stream-интерфейса** — `Random_StreamRandomServer` и т.д.
6. **`RegisterRandomServer`** — функция регистрации на gRPC-сервере.

Всё это — «проводка». Наша задача — реализовать интерфейс `RandomServer`,
остальное делает библиотека.


---

## 📝 Разбор кода

### Общая структура

```go
type server struct {
	pb.UnimplementedRandomServer
}
```

Это **вся** структура сервера. Никаких полей — сервер stateless. Мы
встраиваем `UnimplementedRandomServer` по двум причинам:

1. **Совместимость.** Если в `.proto` завтра добавят пятый метод, наш сервер
   всё равно соберётся — новый метод просто вернёт `Unimplemented`, пока
   мы его не реализуем.
2. **Обязательное требование интерфейса.** В интерфейсе `RandomServer` есть
   приватный метод `mustEmbedUnimplementedRandomServer()`, который нельзя
   реализовать вне пакета. Встраивание заглушки — единственный способ
   выполнить это требование.

### `main` — сборка и запуск

```go
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
```

**`net.Listen("tcp", ":50051")`** — открывает TCP-порт на всех интерфейсах.
Формат `":50051"` означает «слушать на всех IP, порт 50051». Это тот самый
`bind`, который упадёт с `address already in use`, если порт уже занят.

**`grpc.NewServer()`** — создаёт пустой gRPC-сервер. Пока он ничего не умеет:
ни методов, ни маршрутов. Просто заготовка.

**`pb.RegisterRandomServer(s, &server{})`** — вот здесь происходит
«связывание». Функция берёт `Random_ServiceDesc` (таблицу из
сгенерированного файла) и нашу реализацию `&server{}`, и говорит
gRPC-серверу: «когда придут вызовы методов сервиса `random.Random` — вот
объект, у которого их спрашивать».

**`reflection.Register(s)`** — включает gRPC reflection. Это протокол, по
которому сервер отдаёт свою схему по запросу. Благодаря ему `grpcurl` и
`grpcui` работают без локального `.proto`-файла. В проде часто отключают
(безопасность), для учебного проекта — обязательно.

**`s.Serve(lis)`** — блокирующий цикл приёма соединений. На каждое входящее
соединение запускается горутина; внутри неё gRPC обрабатывает HTTP/2-стримы
и вызывает наши методы. `Serve` работает до остановки процесса или ошибки.

### Что происходит при входящем вызове

Проследим путь `GetRandom` от клиента до нашего кода:

1. **Приходит HTTP/2-запрос** с заголовком `:path = /random.Random/GetRandom`.
2. **grpc-go ищет сервис** `random.Random` в своём реестре — находит
   `Random_ServiceDesc`.
3. **По имени метода** `GetRandom` находит обработчик
   `_Random_GetRandom_Handler`.
4. **Обработчик десериализует** байты в `GetRandomRequest`.
5. **Вызывает наш метод** через type assertion:
   `srv.(RandomServer).GetRandom(ctx, req)`.
6. **Наш метод возвращает** `*GetRandomResponse, nil`.
7. **Обработчик сериализует** ответ и отправляет клиенту.

Ничего из этого не происходит в `main` — там только регистрация и цикл
`Serve`. Диспетчеризация полностью автоматическая.

---

### 1. Unary — `GetRandom`

```go
func (s *server) GetRandom(_ context.Context, _ *pb.GetRandomRequest) (*pb.GetRandomResponse, error) {
	n := randInt()
	log.Printf("[Unary] GetRandom → %d", n)
	return &pb.GetRandomResponse{Value: n}, nil
}
```

Самый простой метод. Сигнатура — обычная функция: принимает контекст и
запрос, возвращает ответ и ошибку.

Обратите внимание на подчёркивания вместо имён: `_ context.Context`,
`_ *pb.GetRandomRequest`. Это Go-идиома для неиспользуемых параметров. Нам
ни контекст, ни запрос здесь не нужны — метод не имеет параметров и не
работает с отменой.

**`randInt()`** — вспомогательная функция:

```go
func randInt() int32 {
	return int32(rand.Intn(9) + 1)
}
```

`rand.Intn(9)` даёт `0..8`, прибавляем `1` — получаем `1..9`. Приводим
`int` к `int32`, потому что в `.proto` поле объявлено как `int32 value = 1`.

**`return &pb.GetRandomResponse{Value: n}, nil`** — возвращаем указатель
на структуру и `nil` в качестве ошибки. Если бы метод не смог обработать
запрос, вернули бы `nil, status.Error(codes.Internal, "...")`. Пока всё
просто.

### 2. Server Streaming — `StreamRandom`

```go
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
```

Сигнатура отличается от Unary:

- **Есть `req`** — клиент прислал один запрос (параметры: интервал и лимит).
- **Есть `stream`** — объект `Random_StreamRandomServer`, через который
  отправляются все ответы.
- **Возврат — только `error`** — ответы уходят через `stream.Send`, а не
  через `return`.

**`req.GetIntervalSeconds()`** — геттер. Клиент мог передать `0` — тогда
применяем дефолт `5 секунд`. Это хорошая практика: не полагаться на то, что
клиент всегда пришлёт осмысленное значение.

**`time.NewTicker(interval)`** — тикер, который посылает сигнал в канал
`ticker.C` каждые `interval`. `defer ticker.Stop()` обязателен, иначе
таймер продолжит жить после выхода из метода.

**`select` с двумя ветками:**

- `<-stream.Context().Done()` — клиент отключился или отменил контекст.
  Возвращаем `stream.Context().Err()` — это корректно завершит поток и
  сообщит клиенту причину.
- `<-ticker.C` — пора отправить очередное сообщение. Формируем ответ,
  шлём через `stream.Send`, увеличиваем счётчик.

**`stream.Send(resp)`** — отправка **одного** сообщения. Может возвращать
ошибку, если клиент отключился. Ошибку нужно возвращать наверх — тогда
gRPC корректно закроет поток.

**`if limit > 0 && sent >= limit { return nil }`** — если клиент задал
лимит (например, 5), после отправки пятого сообщения выходим из метода
с `nil`. gRPC закроет поток, и клиент получит `io.EOF` на своём `Recv()`.

**Почему `select`, а не просто `for { ticker.C; Send }`.** Без `select` по
контексту мы бы не заметили отключение клиента до следующего тика. Если
интервал 30 секунд, а клиент отключился сразу после предыдущей отправки —
сервер 30 секунд «висит», держит горутину. С `select` реакция мгновенная.

### 3. Client Streaming — `UploadNumbers`

```go
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
```

Сигнатура **самая необычная**: только `stream`, без `req` и без возврата
ответа в привычном смысле.

- **Нет `req`** — запросов много, они приходят через `stream.Recv()`.
- **Возврат — только `error`** — ответ уходит через `stream.SendAndClose(resp)`.

**Локальные переменные для агрегации.**

```go
var (
	count int32
	sum   int64
	min   int32
	max   int32
	first = true
)
```

Объявлены **внутри метода**, поэтому у каждого клиента свои. Именно поэтому
два клиента, шлющие числа одновременно, не перемешают статистику: они
работают в разных горутинах, с разными наборами переменных.

`sum` — `int64`, а не `int32`: сумма десяти чисел от 1 до 9 не переполнит
`int32`, но в общем случае сумма может быть большой, поэтому запас
не помешает.

`first` — флаг «это первое число». Нужен, чтобы корректно инициализировать
`min` и `max` первым значением. Иначе пришлось бы начинать с
`math.MaxInt32` / `math.MinInt32` — работает, но менее наглядно.

**Цикл `Recv` — сердце метода.**

```go
for {
	req, err := stream.Recv()
	if errors.Is(err, io.EOF) { ... }
	if err != nil { ... }
	// обработка req
}
```

`stream.Recv()` блокируется, пока не придёт следующее сообщение **или**
поток не завершится.

- **`io.EOF`** — клиент вызвал `CloseAndRecv()`, отправка закончена.
  Обрабатываем итог и вызываем `stream.SendAndClose(resp)`.
- **Другая ошибка** — что-то сломалось (сеть, отмена, ошибка на сервере).
  Возвращаем ошибку наверх, поток закроется.

**Обработка `count == 0`** — клиент закрыл поток, ничего не отправив.
Возвращаем пустой `UploadNumbersResponse` со всеми нулями. Это валидный
крайний случай: протокол должен работать и с пустым потоком.

**`stream.SendAndClose(resp)`** — специфика Client Streaming. Отправляет
**одно** ответное сообщение и закрывает поток с серверной стороны. Клиент
получает его из своего `CloseAndRecv()`.

**Порядок `if errors.Is(err, io.EOF)` перед `if err != nil`** —
принципиален. `io.EOF` тоже является `error`, но обрабатывать его надо
**иначе**. Если поменять местами — `io.EOF` попадёт в ветку «ошибка» и
сервер вернёт ошибку вместо итога.

### 4. Bidirectional Streaming — `CompareNumbers`

```go
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
```

Сигнатура — только `stream`, как и в Client Streaming. Разница в том, что
здесь **и читаем, и пишем** в одном цикле.

**`for { Recv; ...; Send }`** — типичный паттерн Bidi на сервере. Читаем
очередное сообщение, отвечаем на него, читаем следующее. Такой сервер
работает как «эхо с логикой»: пришло сообщение — отправили ответ.

**Обратите внимание: сервер отвечает на каждое сообщение.** Но в Bidi это
не обязательное правило. В общем случае сервер мог бы:

- Отвечать на каждое сообщение — как у нас.
- Копить сообщения и отвечать пачкой.
- Отвечать асинхронно, из другой горутины.
- Никогда не отвечать — но тогда это уже не Bidi, а Client Streaming.

Наш вариант — самый простой и наглядный.

**`io.EOF`** — клиент вызвал `CloseSend()`, больше сообщений не будет.
Возвращаем `nil`. gRPC закроет поток с серверной стороны, и клиент получит
`io.EOF` на своём `Recv()`.

**`if err := stream.Send(resp); err != nil`** — если отправка не удалась
(клиент отключился), возвращаем ошибку и завершаем поток. Не пытаемся
продолжать цикл — это бессмысленно.

**Порядок и соответствие ответов.** В нашем коде сервер отвечает на каждое
сообщение сразу, поэтому «запрос N → ответ N». Но это **не гарантия Bidi**,
а следствие нашей реализации. В Bidi протокол не требует такого
соответствия: сервер мог бы отвечать в другом порядке или не отвечать
вовсе.

### Вспомогательная функция `randInt`

```go
func randInt() int32 {
	return int32(rand.Intn(9) + 1)
}
```

Используется во всех четырёх методах. Вынесена отдельно, чтобы не
дублировать логику генерации числа.

**Замечание про `math/rand`.** В Go 1.20+ глобальный источник `math/rand`
автоматически сидируется случайным значением при старте программы —
`rand.Seed(...)` больше не нужен. Ранее это было обязательным шагом, иначе
последовательность была бы одинаковой при каждом запуске.

---

## ▶️ Как запустить

**Из корня проекта:**
```powershell
go run ./cmd/ServerRandom
```

Ожидаемый вывод при старте:
```
gRPC-сервер Random слушает :50051
методы: GetRandom (Unary), StreamRandom (ServerStream), UploadNumbers (ClientStream), CompareNumbers (Bidi)
```

Проверить, что сервер видит все четыре метода:
```powershell
grpcurl -plaintext localhost:50051 describe random.Random
```

Должно вывести что-то вроде:
```
random.Random is a service:
service Random {
  rpc CompareNumbers ( stream .random.CompareNumbersRequest ) returns ( stream .random.CompareNumbersResponse );
  rpc GetRandom ( .random.GetRandomRequest ) returns ( .random.GetRandomResponse );
  rpc StreamRandom ( .random.StreamRandomRequest ) returns ( stream .random.StreamRandomResponse );
  rpc UploadNumbers ( stream .random.UploadNumbersRequest ) returns ( .random.UploadNumbersResponse );
}
```

Список сервисов на порту:
```powershell
grpcurl -plaintext localhost:50051 list
```

Быстрая проверка Unary-метода:
```powershell
grpcurl -plaintext localhost:50051 random.Random/GetRandom
```

Ответ:
```json
{
  "value": 7
}
```

После этого можно в других окнах запускать клиентов — по одному или
нескольким сразу.

---

## 🧪 Что попробовать

**1. Запустить все четыре клиента одновременно.**  
Четыре окна PowerShell, в каждом — свой клиент. В логах сервера будут
перемешаны сообщения от всех четырёх методов, но каждый вызов полностью
изолирован: свои переменные, своя горутина, свой контекст.

**2. Убить сервер во время сеанса.**  
Откройте `ClientServerStream` с `Count: 0` (бесконечный поток), затем
нажмите Ctrl+C в терминале сервера. Клиент получит ошибку `Unavailable`.
Это показывает, что сервер не переподключается сам — соединение рвётся
окончательно.

**3. Запустить два сервера на одном порту.**  
Второй упадёт с `bind: address already in use`. Это фундаментальное
свойство TCP: один порт — один процесс.

**4. Открыть `random_grpc.pb.go` и найти `Random_ServiceDesc`.**  
Там видно, как выглядит таблица маршрутизации. Полезно один раз увидеть
своими глазами, чтобы перестать считать диспетчеризацию магией.

**5. Оставить комментарии в коде про `[Unary]`, `[ServerStream]` и т.д.**  
Они специально с префиксами — при одновременной работе клиентов легко
отфильтровать нужный метод в логах через `Select-String`:
```powershell
go run ./cmd/ServerRandom | Select-String "\[Bidi\]"
```

---

## ⚠️ Типичные ошибки

**`bind: address already in use`**  
На порту 50051 висит старый процесс. Закрыть все окна с `go run` через
диспетчер задач и запустить сервер заново. Не забывайте, что при запуске
клиента против старого сервера клиент получит `EOF` на первом `Send`
(метода нет в старом `ServiceDesc`).

**`cannot use resp.GetValue (value of type func() int32) as int32`**  
Забыли скобки у геттера. `resp.GetValue` — это метод, а не поле.

**Клиент зависает навсегда**  
Скорее всего, сервер не отвечает. Проверьте логи сервера: если для
`UploadNumbers` нет строки `[ClientStream] клиент начал отправку`, значит
до метода вызов не дошёл. Причина — старый сервер на порту или опечатка
в имени метода.

**`SendMsg called after CloseSend`**  
Попытка отправить сообщение после закрытия потока. У нас такого быть не
может, но если в `CompareNumbers` случайно добавить `stream.Send` после
`return` в ветке `io.EOF` — получите эту ошибку.

**Поток в `StreamRandom` не реагирует на отключение клиента**  
Если убрать `select` по `stream.Context().Done()`, сервер не заметит
отключение до следующего тика. При большом интервале горутина будет
«висеть» долго. Это утечка — не делайте так.

**`average: NaN` в `UploadNumbersResponse`**  
Деление `0/0`, если клиент не прислал ни одного числа. У нас есть защита
через `if count == 0`, но в похожем коде легко забыть.

---

## 🧠 Ключевые выводы

1. **Один сервер — четыре паттерна.** Не нужно четыре сервера или четыре
   бинарника. Один `grpc.Server` обслуживает все методы сервиса.
2. **Разница паттернов — только в сигнатурах.** Все четыре метода —
   это методы одной структуры `server`. Различаются они наличием `req` и
   `stream` в аргументах и наличием возврата.
3. **`main` ничего не роутит.** Диспетчеризация — в `Random_ServiceDesc`
   и обработчиках, сгенерированных `protoc`. `main` только регистрирует
   сервис и запускает `Serve`.
4. **Встраивание `UnimplementedRandomServer` — обязательная практика.**
   Даёт совместимость с будущими изменениями `.proto` и удовлетворяет
   требованию приватного метода в интерфейсе.
5. **Локальные переменные = изоляция между клиентами.** `count`, `sum`,
   `min`, `max` объявлены внутри метода `UploadNumbers`, поэтому каждый
   клиент имеет свой набор. Именно поэтому параллельные вызовы не мешают
   друг другу.
6. **`io.EOF` — не ошибка, а сигнал.** В `UploadNumbers` и `CompareNumbers`
   это «клиент закончил отправку». Обрабатывать надо **до** общего
   `if err != nil`.
7. **Отмена через `stream.Context()` — обязательна в `StreamRandom`.**
   Без неё сервер не заметит отключение клиента до следующего тика и
   будет держать горутину зря.
8. **`SendAndClose` — только в Client Streaming.** Единственный метод, где
   сервер отвечает ровно одним сообщением после `io.EOF`. В Bidi — обычный
   `Send`, в Unary — `return`.

---

## 🔗 Сравнение методов сервера

| Метод | Сигнатура | Читает | Пишет | Финал |
|---|---|---|---|---|
| `GetRandom` | `(ctx, req) (resp, err)` | — | — | `return resp` |
| `StreamRandom` | `(req, stream) error` | — | `stream.Send` много раз | `return nil` |
| `UploadNumbers` | `(stream) error` | `stream.Recv` в цикле | `stream.SendAndClose` | после `io.EOF` |
| `CompareNumbers` | `(stream) error` | `stream.Recv` в цикле | `stream.Send` в цикле | после `io.EOF` |

Запомните эти четыре сигнатуры — они универсальны для любого gRPC-сервера
в Go. Меняются только имена методов и типы сообщений, каркас всегда такой.

---

[Главный](Readme.md)