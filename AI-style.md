# Детальный анализ и рекомендации по улучшению библиотеки httpq

## Обзор

Библиотека `httpq` представляет собой HTTP-клиент с fluent API для Go. Анализ выявил **критические проблемы безопасности и корректности**, которые необходимо исправить перед использованием в production.

---

## 🔴 КРИТИЧЕСКИЕ ПРОБЛЕМЫ (требуют немедленного исправления)

### 1. Мутация глобального `http.DefaultClient` — КРИТИЧНО

**Проблема:**
```119:125:httpq.go
func NewRpc() *Rpc {
	return &Rpc{
		contentType:       ContentJson,
		contentTypeString: "application/json",
		client:            http.DefaultClient,
	}
}
```

```154:157:httpq.go
func (r *Rpc) SetTimeout(timeout time.Duration) *Rpc {
	r.client.Timeout = timeout
	return r
}
```

**Суть проблемы:**
- `NewRpc()` присваивает `r.client = http.DefaultClient` (общий указатель)
- При вызове `SetTimeout()` изменяется `http.DefaultClient.Timeout` глобально
- Это влияет на **все HTTP-запросы в процессе**, использующие `http.DefaultClient`
- В конкурентной среде разные горутины будут перезаписывать настройки друг друга

**Последствия:**
- Непредсказуемое поведение всей программы
- Race conditions при конкурентном использовании
- Невозможность изолировать настройки для разных запросов

**Решение:**
```go
func NewRpc() *Rpc {
	return &Rpc{
		contentType:       ContentJson,
		contentTypeString: "application/json",
		client:            &http.Client{}, // Создаём новый экземпляр
	}
}
```

**Дополнительно:** Метод `Clone()` также должен создавать новый клиент:
```go
func (r *Rpc) Clone() *Rpc {
	// ...
	clone := &Rpc{
		client:            &http.Client{
			Transport:     r.client.Transport,
			CheckRedirect: r.client.CheckRedirect,
			Timeout:       r.client.Timeout,
		},
		// ...
	}
	// ...
}
```

---

### 2. `panic` в библиотечном коде — КРИТИЧНО

**Проблема:**
```395:410:httpq.go
case ContentMultiPart:
	values := map[string]any{}
	bts, errMarshal := json.Marshal(r.body)
	if errMarshal != nil {
		panic(errMarshal)  // ❌ КРИТИЧЕСКАЯ ОШИБКА
	}
	_ = json.Unmarshal(bts, &values)
	w := multipart.NewWriter(body)
	for k, v := range values {
		wfield, _ := w.CreateFormField(k)
		_, _ = wfield.Write([]byte(fmt.Sprintf("%v", v)))
	}

	r.contentTypeString = w.FormDataContentType()
	_ = w.Close()
```

**Суть проблемы:**
- HTTP-клиент **не должен паниковать** — это нарушает контракт библиотеки
- Если `r.body` содержит несериализуемые данные (каналы, функции), приложение упадёт
- Нет способа обработать ошибку на стороне вызывающего кода

**Решение:**
Изменить сигнатуру `doOnce` для возврата ошибки:
```go
func doOnce[T any](ctx context.Context, r *Rpc, tID string, start time.Time) (result *ResponseModel[T], statusCode int, err error) {
	// ...
	case ContentMultiPart:
		values := map[string]any{}
		bts, errMarshal := json.Marshal(r.body)
		if errMarshal != nil {
			return nil, 0, fmt.Errorf("multipart: failed to marshal body: %w", errMarshal)
		}
		if err := json.Unmarshal(bts, &values); err != nil {
			return nil, 0, fmt.Errorf("multipart: failed to unmarshal body: %w", err)
		}
		// ...
}
```

---

### 3. Игнорирование ошибок сериализации — КРИТИЧНО

**Проблема:**
```385:390:httpq.go
case ContentJson:
	_ = json.NewEncoder(body).Encode(r.body)  // ❌ Ошибка игнорируется
	r.contentTypeString = "application/json"
case ContentXml:
	_ = xml.NewEncoder(body).Encode(r.body)  // ❌ Ошибка игнорируется
	r.contentTypeString = "application/xml"
```

**Суть проблемы:**
- Если сериализация упала, запрос уйдёт с **пустым телом**
- Клиент не узнает об ошибке
- Это критическая логическая ошибка

**Решение:**
```go
case ContentJson:
	if err := json.NewEncoder(body).Encode(r.body); err != nil {
		return nil, 0, fmt.Errorf("json: failed to encode body: %w", err)
	}
	r.contentTypeString = "application/json"
case ContentXml:
	if err := xml.NewEncoder(body).Encode(r.body); err != nil {
		return nil, 0, fmt.Errorf("xml: failed to encode body: %w", err)
	}
	r.contentTypeString = "application/xml"
```

---

### 4. RetryPolicy не работает по умолчанию — КРИТИЧНО

**Проблема:**
```79:96:httpq.go
func isRetryableStatus(policy *RetryPolicy, code int) bool {
	// Connection-level error (we treat 0 as "no HTTP response").
	if code == 0 {
		return true
	}

	// If policy is not set or has no explicit codes, do not retry based on status.
	if policy == nil || len(policy.RetryStatusCodes) == 0 {
		return false  // ❌ Даже для 5xx не будет ретрая!
	}

	for _, c := range policy.RetryStatusCodes {
		if c == code {
			return true
		}
	}
	return false
}
```

**Суть проблемы:**
- Если `RetryStatusCodes` не задан, ретраи **никогда не выполняются**, даже для 5xx
- `DefaultRetryable5xx` и `DefaultRecommendedRetryStatusCodes` объявлены, но **не используются по умолчанию**
- Пользователь может ожидать retry, но его не будет

**Решение:**
```go
func isRetryableStatus(policy *RetryPolicy, code int) bool {
	// Connection-level error (we treat 0 as "no HTTP response").
	if code == 0 {
		return true
	}

	if policy == nil {
		return false
	}

	// Если RetryStatusCodes не задан, используем дефолтные значения
	codes := policy.RetryStatusCodes
	if len(codes) == 0 {
		codes = DefaultRetryable5xx
	}

	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}
```

**Альтернативный подход:** Использовать дефолтные значения при создании политики:
```go
func (r *Rpc) SetRetryPolicy(policy *RetryPolicy) *Rpc {
	if policy != nil && len(policy.RetryStatusCodes) == 0 {
		// Копируем политику, чтобы не мутировать оригинал
		policyCopy := *policy
		policyCopy.RetryStatusCodes = DefaultRetryable5xx
		r.retryPolicy = &policyCopy
	} else {
		r.retryPolicy = policy
	}
	return r
}
```

---

### 5. Data Race: мутация `contentTypeString` во время выполнения

**Проблема:**
```379:411:httpq.go
func doOnce[T any](ctx context.Context, r *Rpc, tID string, start time.Time) (result *ResponseModel[T], statusCode int, err error) {
	body := new(bytes.Buffer)

	// (Re)build body and content type for each attempt.
	if r.body != nil {
		switch r.contentType {
		case ContentJson:
			_ = json.NewEncoder(body).Encode(r.body)
			r.contentTypeString = "application/json"  // ❌ Мутация структуры
		case ContentXml:
			_ = xml.NewEncoder(body).Encode(r.body)
			r.contentTypeString = "application/xml"  // ❌ Мутация структуры
		// ...
		case ContentMultiPart:
			// ...
			r.contentTypeString = w.FormDataContentType()  // ❌ Мутация структуры
		}
	}
```

**Суть проблемы:**
- `r.contentTypeString` изменяется внутри `doOnce`
- Если `Rpc` переиспользуется или используется конкурентно → возможен **data race**
- Это нарушает thread-safety

**Решение:**
Использовать локальную переменную вместо мутации структуры:
```go
func doOnce[T any](ctx context.Context, r *Rpc, tID string, start time.Time) (result *ResponseModel[T], statusCode int, err error) {
	body := new(bytes.Buffer)
	var contentTypeString string

	if r.body != nil {
		switch r.contentType {
		case ContentJson:
			if err := json.NewEncoder(body).Encode(r.body); err != nil {
				return nil, 0, fmt.Errorf("json: failed to encode body: %w", err)
			}
			contentTypeString = "application/json"
		case ContentXml:
			if err := xml.NewEncoder(body).Encode(r.body); err != nil {
				return nil, 0, fmt.Errorf("xml: failed to encode body: %w", err)
			}
			contentTypeString = "application/xml"
		case ContentBytes:
			if b, ok := r.body.([]byte); ok {
				_, _ = body.Write(b)
			}
		case ContentMultiPart:
			// ... обработка multipart
			contentTypeString = w.FormDataContentType()
		}
	} else {
		contentTypeString = r.contentTypeString
	}

	// Использовать contentTypeString вместо r.contentTypeString
	req.Header.Set("Content-Type", contentTypeString)
	// ...
}
```

---

## 🟡 СРЕДНИЕ ПРОБЛЕМЫ (требуют исправления)

### 6. Неэффективная реализация MultiPart

**Проблема:**
```395:401:httpq.go
case ContentMultiPart:
	values := map[string]any{}
	bts, errMarshal := json.Marshal(r.body)
	if errMarshal != nil {
		panic(errMarshal)
	}
	_ = json.Unmarshal(bts, &values)  // ❌ Двойное копирование данных
```

**Суть проблемы:**
- Двойное копирование данных в памяти (Marshal → Unmarshal)
- Огромные затраты CPU и памяти
- Потеря типизации

**Решение:**
Использовать рефлексию или более эффективный подход:
```go
case ContentMultiPart:
	w := multipart.NewWriter(body)
	
	// Использовать рефлексию для прямого преобразования
	rv := reflect.ValueOf(r.body)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, 0, fmt.Errorf("multipart: body must be a struct or pointer to struct")
	}
	
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		value := rv.Field(i)
		
		// Пропускаем приватные поля
		if !value.CanInterface() {
			continue
		}
		
		fieldName := field.Tag.Get("form")
		if fieldName == "" {
			fieldName = strings.ToLower(field.Name)
		}
		
		wfield, err := w.CreateFormField(fieldName)
		if err != nil {
			return nil, 0, fmt.Errorf("multipart: failed to create field: %w", err)
		}
		
		fmt.Fprintf(wfield, "%v", value.Interface())
	}
	
	contentTypeString = w.FormDataContentType()
	if err := w.Close(); err != nil {
		return nil, 0, fmt.Errorf("multipart: failed to close writer: %w", err)
	}
```

---

### 7. Потенциальная утечка ресурсов (Body leak)

**Проблема:**
```465:471:httpq.go
defer resp.Body.Close()

statusCode = resp.StatusCode

var bodyBytes []byte
if resp.Body != http.NoBody {
	bodyBytes, _ = io.ReadAll(resp.Body)
```

**Суть проблемы:**
- Хотя `defer resp.Body.Close()` закрывает тело, для Keep-Alive соединений рекомендуется полностью вычитать тело
- Если `io.ReadAll` вернёт ошибку, тело может быть не полностью прочитано

**Решение:**
```go
defer resp.Body.Close()

statusCode = resp.StatusCode

var bodyBytes []byte
if resp.Body != http.NoBody {
	var readErr error
	bodyBytes, readErr = io.ReadAll(resp.Body)
	if readErr != nil {
		// Убеждаемся, что тело полностью прочитано для Keep-Alive
		io.Copy(io.Discard, resp.Body)
		return nil, statusCode, fmt.Errorf("failed to read response body: %w", readErr)
	}
}
```

---

### 8. Потеря multi-value HTTP заголовков

**Проблема:**
```508:518:httpq.go
result = &ResponseModel[T]{
	StatusCode:  resp.StatusCode,
	Headers:     map[string]string{},  // ❌ Только первое значение
	ContentType: ct,
	RawBody:     bodyBytes,
}

// Копируем заголовки
for k, v := range resp.Header {
	if len(v) > 0 {
		result.Headers[k] = v[0]  // ❌ Теряются повторяющиеся заголовки
	}
}
```

**Суть проблемы:**
- Теряются повторяющиеся заголовки (например, несколько `Set-Cookie`)
- Невозможно получить все значения заголовка

**Решение:**
Изменить тип `Headers` в `ResponseModel`:
```go
type ResponseModel[T any] struct {
	Data        T
	StatusCode  int
	Headers     map[string][]string  // Изменить на []string
	ContentType string
	RawBody     []byte
}

// В doOnce:
result.Headers = make(map[string][]string)
for k, v := range resp.Header {
	result.Headers[k] = v  // Сохраняем все значения
}
```

**Обратная совместимость:** Можно добавить метод-хелпер:
```go
func (r *ResponseModel[T]) GetHeader(name string) string {
	if values := r.Headers[name]; len(values) > 0 {
		return values[0]
	}
	return ""
}
```

---

## 🟢 УЛУЧШЕНИЯ (желательно, но не критично)

### 9. Добавить ParseError в ResponseModel

**Проблема:**
Невозможно отличить пустой ответ от ошибки парсинга.

**Решение:**
```go
type ResponseModel[T any] struct {
	Data        T
	StatusCode  int
	Headers     map[string][]string
	ContentType string
	RawBody     []byte
	ParseError  error  // Ошибка парсинга, если была
}
```

---

### 10. Улучшить логирование бинарных тел

**Проблема:**
```421:423:httpq.go
"body", body.String(),  // ❌ Опасно для бинарных данных и больших payload
```

**Решение:**
```go
if r.logging {
	args := []any{
		"method", r.method,
		"url", r.url,
		"trace_id", tID,
		"content_type", contentTypeString,
		"headers", r.header,
	}
	
	// Логируем тело только для текстовых типов и ограниченного размера
	if body.Len() > 0 && body.Len() < 1024 {
		bodyStr := body.String()
		if isTextContent(contentTypeString) {
			args = append(args, "body", bodyStr)
		} else {
			args = append(args, "body_size", body.Len(), "body_preview", bodyStr[:min(100, len(bodyStr))])
		}
	}
	
	r.getLogger().InfoContext(ctx, "httpq: request", args...)
}

func isTextContent(ct string) bool {
	return strings.HasPrefix(ct, "text/") ||
		strings.Contains(ct, "json") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "html")
}
```

---

### 11. Добавить дефолтный User-Agent

**Рекомендация:**
```go
const DefaultUserAgent = "httpq/v3"

func doOnce[T any](...) {
	// ...
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", DefaultUserAgent)
	}
	// ...
}
```

---

### 12. Добавить jitter для retry

**Рекомендация:**
```go
func nextRetryDelay(policy *RetryPolicy, attempt int) time.Duration {
	// ... существующая логика ...
	delay := baseDelay
	
	// Добавить jitter (±20%)
	if policy.Jitter {
		jitter := time.Duration(float64(delay) * 0.2 * (rand.Float64()*2 - 1))
		delay += jitter
	}
	
	return delay
}
```

---

## 📋 ПЛАН ИСПРАВЛЕНИЙ (приоритет)

### Фаза 1: Критические исправления (немедленно)
1. ✅ Исправить использование `http.DefaultClient` → создать новый клиент
2. ✅ Убрать `panic` из `ContentMultiPart` → возвращать ошибку
3. ✅ Обработать ошибки сериализации JSON/XML
4. ✅ Исправить логику `isRetryableStatus` для работы по умолчанию
5. ✅ Убрать мутацию `contentTypeString` в `doOnce`

### Фаза 2: Средние исправления (в ближайшее время)
6. ✅ Оптимизировать реализацию `ContentMultiPart`
7. ✅ Улучшить обработку тела ответа для Keep-Alive
8. ✅ Исправить потерю multi-value заголовков

### Фаза 3: Улучшения (по возможности)
9. ✅ Добавить `ParseError` в `ResponseModel`
10. ✅ Улучшить логирование бинарных тел
11. ✅ Добавить дефолтный User-Agent
12. ✅ Добавить jitter для retry

---

## 🧪 ТЕСТИРОВАНИЕ

После исправлений необходимо добавить тесты для:
- Конкурентного использования `Rpc` (data race detection)
- Поведения при ошибках сериализации
- Retry-логики с дефолтными значениями
- Изоляции `http.Client` между экземплярами

---

## 📝 ЗАКЛЮЧЕНИЕ

Библиотека имеет хорошую основу (fluent API, generics, retry), но содержит **критические проблемы безопасности и корректности**, которые делают её небезопасной для production без исправлений.

**Рекомендация:** Исправить все проблемы Фазы 1 перед использованием в production.

