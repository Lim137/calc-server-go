# calc-server-go

Go-переписывание тестового сервиса `calculator_server` + `generator`, изначально написанного на Python. Сервер обновляет два счётчика через вызовы внешних библиотек на C и Rust, отдаёт метрики в формате Prometheus.

## Требования для сборки

- Go 1.21+
- gcc (или другой C-компилятор)
- Rust toolchain (`cargo`)

## Сборка

```bash
./build.sh
```

Соберёт по порядку:
1. `libcalculator.so` — C-библиотека (`c_lib/calculator.c`)
2. `libcalculator_rust.so` — Rust-библиотека (`rust_lib/`)
3. `calculator_server` — Go-бинарник сервера
4. `generator` — Go-бинарник нагрузочного генератора

## Запуск

Сервер:

```bash
./calculator_server --port 8080
```

Флаги: `--host` (по умолчанию `0.0.0.0`), `--port` (по умолчанию `8080`), `--interval` (интервал печати текущих значений `sum`/`sub` в консоль, по умолчанию `5s`).

Генератор нагрузки (в другом терминале):

```bash
./generator --url http://localhost:8080/calc --threads 10 --interval 100ms
```

Флаги: `--url`, `--threads` (число горутин-воркеров, по умолчанию `10`), `--interval` (пауза между запросами на воркер, `0` — без пауз), `--timeout` (таймаут HTTP-запроса).

## Проверка результата

Одиночный запрос:

```bash
curl -X POST "http://localhost:8080/calc?num=5"
```

Метрики:

```bash
curl http://localhost:8080/metrics
```

В выводе будут:
- `http_requests_per_second{seconds_ago="0..59"}` — количество запросов `/calc` за каждую из последних 60 секунд
- `c_call_duration_seconds{quantile="0.95"|"0.99"}` — p95/p99 времени выполнения вызова C-функции `add`
- `rust_call_duration_seconds{quantile="0.95"|"0.99"}` — p95/p99 времени выполнения вызова Rust-функции `sub`

Остановка обоих процессов — `Ctrl+C` (корректно завершаются, сервер и генератор печатают итоговую статистику).
