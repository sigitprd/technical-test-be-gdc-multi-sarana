# Task Management API (Multi-User)

REST API untuk manajemen task multi-user: setiap user mengelola task miliknya sendiri dan bisa meng-assign task ke anggota tim yang sama.

**Stack:** Go 1.24 · Echo v4 · PostgreSQL 16 (pgx v5) · JWT (HS256) · bcrypt · `log/slog` (JSON) · Docker Compose · Makefile

---

## Daftar Isi

- [Cara Menjalankan](#cara-menjalankan)
- [Konfigurasi](#konfigurasi)
- [Daftar Endpoint](#daftar-endpoint)
- [Contoh Penggunaan](#contoh-penggunaan)
- [Arsitektur](#arsitektur)
- [Database Design](#database-design)
- [Implementasi Requirement](#implementasi-requirement)
- [Perintah Makefile](#perintah-makefile)

---

## Cara Menjalankan

Prasyarat: Docker + Docker Compose v2.20 atau lebih baru. Go 1.24 hanya dibutuhkan untuk menjalankan tanpa Docker. [Contoh penggunaan](#contoh-penggunaan) memakai `curl` dan `jq`, sedangkan e2e test butuh `curl` dan `python3`.

```bash
cp .env.example .env    # opsional, `make` membuat .env otomatis jika belum ada
```

`make` membuat `.env` dari `.env.example` dengan `JWT_SECRET` acak. Kalau `.env` disalin manual, ganti `JWT_SECRET` dengan nilai acak minimal 32 karakter (`openssl rand -hex 32`). Nilai contoh dari `.env.example` ditolak saat `APP_ENV=production`.

### Opsi A: API + PostgreSQL bawaan (paling mudah)

```bash
make up
```

- API: `http://localhost:8080`
- PostgreSQL container di-publish ke `localhost:5433` (bukan 5432, supaya tidak bentrok dengan PostgreSQL lokal).
- Migration berjalan otomatis saat API start.

### Opsi B: API di Docker, PostgreSQL yang sudah ada

Untuk penguji yang sudah punya PostgreSQL sendiri. Buat database kosong, lalu isi di `.env`:

```dotenv
# Host PostgreSQL dilihat dari dalam container API
DB_HOST_DOCKER=host.docker.internal
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=task_management
```

```bash
make up-external-db
```

Migration membutuhkan extension `pg_trgm` (trusted extension sejak PostgreSQL 13, jadi owner database bisa membuatnya). Versi minimum PostgreSQL: 13.

### Opsi C: API langsung di host (tanpa Docker untuk API)

```bash
make dev    # start PostgreSQL bawaan di Docker, lalu `go run` API di host
# atau, jika PostgreSQL sudah ada dan DB_HOST/DB_PORT di .env sudah benar:
make run
```

### Menghentikan

```bash
make down            # stop container
make down-volumes    # stop + hapus data PostgreSQL bawaan
```

### Unit Test

Unit test tidak butuh database maupun service eksternal.

```bash
make test               # semua unit test
make test-race          # semua unit test dengan race detector
make test-idempotency   # khusus test idempotency dan concurrency, dengan race detector
make cover              # coverage per package + total, gagal jika total di bawah 95%
```

Coverage saat ini **98.0%**, diukur dengan `-coverpkg` atas seluruh package production, termasuk repository, `internal/app`, dan `cmd/api`. Yang dikecualikan hanya `internal/service/servicetest`, yaitu fake untuk test. Detail baris per baris ada di `coverage.html` setelah `make cover`. Lihat juga [5. Unit Testing](#5-unit-testing).

### End-to-end Test

Selain unit test, ada smoke test end-to-end ([scripts/e2e.sh](scripts/e2e.sh)) yang memanggil API sungguhan: auth dan invite code tim, JWT, idempotency (sequential, 30 request paralel, window 24 jam), CRUD, filter/search/pagination, hak akses, assign + `task_logs`, format error, dan structured log. Butuh `curl` dan `python3`.

```bash
make up     # stack harus jalan
make e2e
```

Setiap run memakai user baru, jadi bisa diulang tanpa reset database. Satu run mengirim sekitar 19 request ke `/auth`, masih di bawah rate limit default (60 per menit). Rate limit sendiri tidak diuji di sini karena akan mengunci `/auth` selama satu menit. Cek yang butuh akses langsung ke PostgreSQL atau log container API (misalnya `task_logs`, key kedaluwarsa, level log) otomatis di-skip jika container tidak ditemukan, misalnya pada mode `make up-external-db`.

Rollback transaksi assign diuji manual, karena butuh API dengan notifier yang dibuat gagal. Perintah pertama menyalakan simulasi tanpa mengubah `.env`. Perintah terakhir membuat ulang container dengan nilai dari `.env`, jadi simulasi baru benar-benar mati kalau `.env` berisi `NOTIFIER_SIMULATE_FAILURE=false` (default). Bentuk log kedua mode ada di [requirement 3](#3-database-transaction--integrity-post-tasksidassign).

```bash
NOTIFIER_SIMULATE_FAILURE=true docker compose -f docker-compose.yml -f docker-compose.db.yml up -d api
# POST /api/v1/tasks/:id/assign -> 500, assignee_id tidak berubah, task_logs tidak bertambah
docker compose -f docker-compose.yml -f docker-compose.db.yml up -d api
```

---

## Konfigurasi

Semua konfigurasi lewat environment variable (lihat [.env.example](.env.example)). Aplikasi menolak start jika konfigurasi tidak valid, misalnya `JWT_SECRET` kurang dari 32 karakter.

| Variable | Default | Keterangan |
|---|---|---|
| `APP_ENV` | `development` | `development` / `production`. Di production, field `debug` tidak pernah dikirim di error response |
| `HTTP_PORT` | `8080` | Port HTTP |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `DATABASE_URL` | – | Jika di-set, menggantikan semua `DB_*` |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSLMODE` | `localhost`, `5432`, `postgres`, `postgres`, `task_management`, `disable` | Koneksi PostgreSQL |
| `DB_HOST_DOCKER` | `host.docker.internal` | Host DB untuk container API pada `make up-external-db` |
| `DB_AUTO_MIGRATE` | `true` | Jalankan migration saat start |
| `DB_MAX_CONNS` | `10` | Batas koneksi di connection pool |
| `DB_CONNECT_RETRIES` | `10` | Jumlah percobaan koneksi DB saat start. Jeda bertambah 1 detik tiap percobaan, supaya API tetap bisa start walaupun database masih booting |
| `JWT_SECRET` | – (wajib) | Secret HS256, minimal 32 karakter. Nilai contoh ditolak di production |
| `JWT_TTL` | `24h` | Masa berlaku access token |
| `AUTH_RATE_LIMIT` | `60` | Request per menit per IP ke `/auth/register` dan `/auth/login`. `0` mematikan rate limit |
| `IDEMPOTENCY_TTL` | `24h` | Window idempotency key |
| `NOTIFIER_SIMULATE_FAILURE` | `false` | `true` membuat mock notifier selalu gagal, jadi assign dijawab `500` dan di-rollback. Hanya untuk demo rollback; biarkan `false` untuk pemakaian normal |

Perubahan `.env` baru terbaca setelah container API dibuat ulang: jalankan lagi `make up` (atau `make up-external-db`). Untuk `make run` dan `make dev`, cukup jalankan ulang perintahnya.

---

## Daftar Endpoint

Base URL: `http://localhost:8080`. Endpoint bertanda 🔒 wajib header `Authorization: Bearer <access_token>`. Endpoint di soal (`/tasks`, `/tasks/:id`, `/tasks/:id/assign`) ada di bawah prefix `/api/v1`, misalnya `POST /api/v1/tasks`.

| Method | Path | Keterangan |
|---|---|---|
| GET | `/health` | Health check (cek koneksi DB) |
| POST | `/api/v1/auth/register` | Register user: buat tim baru (`team_name`) atau join tim (`invite_code`) |
| POST | `/api/v1/auth/login` | Login, mendapat JWT |
| GET | `/api/v1/users/me` 🔒 | Profil user yang login + timnya (termasuk `invite_code`) |
| GET | `/api/v1/users/team-members` 🔒 | Anggota tim (kandidat assignee) |
| POST | `/api/v1/tasks` 🔒 | Create task. Mendukung header `Idempotency-Key` (UUID) |
| GET | `/api/v1/tasks` 🔒 | List task: `?status=&search=&page=&limit=` |
| GET | `/api/v1/tasks/:id` 🔒 | Detail task |
| PUT | `/api/v1/tasks/:id` 🔒 | Update task sebagian: hanya field yang dikirim yang berubah |
| DELETE | `/api/v1/tasks/:id` 🔒 | Delete task |
| POST | `/api/v1/tasks/:id/assign` 🔒 | Assign task ke anggota tim yang sama (transactional) |

### Tim dan invite code

- Register dengan `team_name` membuat tim baru. Response-nya berisi `team.invite_code`.
- User lain join ke tim itu dengan register memakai `invite_code` tersebut (huruf besar/kecil tidak berpengaruh).
- Nama tim tidak unik dan tidak bisa dipakai untuk join, jadi orang luar tidak bisa masuk ke tim lain hanya dengan menebak namanya.
- Kirim tepat salah satu dari `team_name` atau `invite_code`. Keduanya atau tidak sama sekali: `400 INVALID_TEAM_CHOICE`. Kode yang tidak dikenal: `422 INVALID_INVITE_CODE`.
- Semua anggota bisa melihat invite code timnya di `GET /users/me`.

### Aturan akses

- **Lihat** (list/detail): owner atau assignee task.
- **Ubah, hapus, assign**: hanya owner. Assignee mendapat `403 TASK_FORBIDDEN`.
- Task milik orang lain yang tidak terlihat oleh user dijawab `404 TASK_NOT_FOUND`, supaya keberadaan task tidak bocor.
- **Assign** hanya ke anggota lain di tim yang sama. Ke owner sendiri: `422 ASSIGNEE_IS_OWNER`. Ke user tim lain: `422 ASSIGNEE_NOT_IN_TEAM`. Assign ulang ke assignee yang sama tidak mengubah apa pun, jadi tidak ada log atau notifikasi baru.

### Update task (PUT)

PUT menerima update sebagian, jadi body cukup berisi field yang mau diubah.

- Field yang tidak dikirim tidak berubah. Contoh: `{"status":"done"}` hanya mengubah status.
- `"due_date": null` menghapus due date.
- `title`, kalau dikirim, tidak boleh kosong. `status`, kalau dikirim, harus `todo`, `in_progress`, atau `done`.
- Body tanpa field sama sekali: `400 NO_FIELDS_TO_UPDATE`.
- Row task dikunci (`SELECT ... FOR UPDATE`) selama update, jadi dua update bersamaan pada field berbeda tidak saling menimpa.

### Query list task

| Param | Keterangan |
|---|---|
| `status` | `todo` / `in_progress` / `done` |
| `search` | Pencarian `title` (case-insensitive, substring; karakter `%` dan `_` dicari secara literal) |
| `page` | Default `1`, maksimal `1000000` |
| `limit` | Default `10`, maksimal `100` |

Hasil diurutkan dari task terbaru.

### Format response

Sukses:

```json
{ "data": { ... } }
{ "data": [ ... ], "meta": { "page": 1, "limit": 10, "total": 42, "total_pages": 5 } }
```

Error (semua error, termasuk 404 route, 405, body terlalu besar, dan panic):

```json
{
  "status": 400,
  "code": "VALIDATION_ERROR",
  "message": "request validation failed",
  "details": [{ "field": "email", "message": "must be a valid email address" }],
  "request_id": "0b7e2a1c-1d7f-4f61-9a43-6f1f6c7c2b1e",
  "timestamp": "2026-10-13T01:03:09.431685717Z"
}
```

Kode error utama:

| HTTP | Code |
|---|---|
| 400 | `VALIDATION_ERROR`, `INVALID_REQUEST_BODY`, `INVALID_QUERY_PARAMS`, `INVALID_ID`, `INVALID_IDEMPOTENCY_KEY`, `INVALID_TEAM_CHOICE`, `NO_FIELDS_TO_UPDATE` |
| 401 | `MISSING_TOKEN`, `INVALID_TOKEN`, `INVALID_CREDENTIALS` |
| 403 | `TASK_FORBIDDEN` |
| 404 | `TASK_NOT_FOUND`, `USER_NOT_FOUND`, `NOT_FOUND` |
| 405 | `METHOD_NOT_ALLOWED` |
| 409 | `EMAIL_ALREADY_REGISTERED` |
| 413 | `PAYLOAD_TOO_LARGE` |
| 422 | `INVALID_INVITE_CODE`, `ASSIGNEE_NOT_FOUND`, `ASSIGNEE_NOT_IN_TEAM`, `ASSIGNEE_IS_OWNER`, `IDEMPOTENCY_KEY_REUSED` |
| 429 | `RATE_LIMIT_EXCEEDED` |
| 500 | `INTERNAL_SERVER_ERROR` |
| 503 | `SERVICE_UNAVAILABLE` |

---

## Contoh Penggunaan

Blok berikut bisa langsung di-copy-paste setelah `make up`. Id yang dibutuhkan ditangkap otomatis dengan `jq`. Email di contoh ini tetap, jadi jalankan di database kosong (`make down-volumes && make up`) atau ganti emailnya. Assign hanya berhasil kalau `NOTIFIER_SIMULATE_FAILURE=false`.

```bash
BASE=http://localhost:8080/api/v1
JSON='Content-Type: application/json'

# Alice membuat tim baru; simpan invite code dari response
INVITE=$(curl -s -X POST $BASE/auth/register -H "$JSON" \
  -d '{"name":"Alice","email":"alice@example.com","password":"password123","team_name":"Alpha"}' | jq -r .data.team.invite_code)

# Bob join ke tim Alice dengan invite code; simpan id Bob
BOB=$(curl -s -X POST $BASE/auth/register -H "$JSON" \
  -d "{\"name\":\"Bob\",\"email\":\"bob@example.com\",\"password\":\"password123\",\"invite_code\":\"$INVITE\"}" | jq -r .data.id)

# Login sebagai Alice
TOKEN=$(curl -s -X POST $BASE/auth/login -H "$JSON" \
  -d '{"email":"alice@example.com","password":"password123"}' | jq -r .data.access_token)
AUTH="Authorization: Bearer $TOKEN"

# Create task dengan Idempotency-Key; simpan id task
KEY=6f1c2a9e-3b6d-4c1a-9f0e-2d5b8a7c4e11
BODY='{"title":"Write report","description":"Q3 report","due_date":"2026-10-20T10:00:00+07:00"}'
TASK=$(curl -s -X POST $BASE/tasks -H "$AUTH" -H "$JSON" -H "Idempotency-Key: $KEY" -d "$BODY" | jq -r .data.id)

# Kirim ulang request yang sama: 201 dengan body identik dan header Idempotent-Replayed: true, tanpa task baru
curl -si -X POST $BASE/tasks -H "$AUTH" -H "$JSON" -H "Idempotency-Key: $KEY" -d "$BODY"

# List dengan filter, search, pagination
curl -s "$BASE/tasks?status=todo&search=report&page=1&limit=10" -H "$AUTH" | jq .

# Update sebagian: hanya status yang berubah
curl -s -X PUT $BASE/tasks/$TASK -H "$AUTH" -H "$JSON" -d '{"status":"in_progress"}' | jq .

# Assign ke Bob, lalu lihat notifikasinya di log API
curl -s -X POST $BASE/tasks/$TASK/assign -H "$AUTH" -H "$JSON" -d "{\"assignee_id\":\"$BOB\"}" | jq .
docker logs task-management-api 2>&1 | grep '"notification sent"' | tail -1 | jq .

# Delete (204)
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE $BASE/tasks/$TASK -H "$AUTH"
```

---

## Arsitektur

Clean architecture berlapis. Dependency hanya mengarah ke dalam: handler bergantung pada interface service, service bergantung pada interface repository (port) yang dideklarasikan di package service sendiri.

```
HTTP request
   │
   ▼
middleware   RequestLogger (request_id + JSON log) → Recover (panic) → Secure headers → BodyLimit → RateLimit (/auth) atau JWTAuth
   │
   ▼
handler      bind + validasi DTO, mapping ke input use case, render response
   │  (interface AuthService / TaskService / UserService)
   ▼
service      business rules, otorisasi, transaksi, idempotency
   │  (interface TxManager / *Repository / Notifier / KeyLocker / Clock)
   ▼
repository   PostgreSQL via pgx. Transaksi dibawa lewat context.Context
```

```
cmd/api/main.go                     entrypoint: load config, logger, signal handling
internal/
  app/                              composition root: wiring dependency, HTTP server + cleanup job, graceful shutdown
  config/                           load + validasi env
  domain/                           entity (User, Team, Task, TaskLog, IdempotencyRecord), tanpa tag JSON/DB
  apperror/                         tipe error aplikasi (kind, code, message, details, cause), tanpa HTTP
  service/                          use case + port (interface) + error bisnis
    servicetest/                    fake in-memory untuk port service, khusus test
  repository/postgres/              implementasi repository, TxManager, migrator, migrations/*.sql
  notifier/                         implementasi Notifier (mock: menulis notifikasi ke log)
  transport/http/                   router Echo
    handler/                        handler, DTO, validator
    middleware/                     request logger, recover, JWT auth, global error handler
    response/                       envelope JSON
  pkg/                              utilitas generik tanpa pengetahuan domain: logger, token (JWT), hasher (bcrypt), keylock, concurrent (SafeGroup)
scripts/                            e2e.sh (end-to-end test), cover.sh (coverage per package)
```

Keputusan desain:

- **Inner layer bebas framework.** `domain`, `apperror`, dan `service` tidak meng-import `net/http`, Echo, maupun pgx. Bisa dicek dengan `go list -f '{{join .Imports "\n"}}' ./internal/service`.
- **Error transport-agnostic.** Use case mengembalikan `apperror.Error` dengan `Kind` (`NotFound`, `Forbidden`, `Conflict`, ...). Pemetaan `Kind` ke HTTP status hanya ada di [error_handler.go](internal/transport/http/middleware/error_handler.go), jadi delivery lain (gRPC, CLI, worker) cukup membuat pemetaannya sendiri.
- **Entity bebas urusan serialisasi.** Bentuk JSON untuk response ada di DTO handler, sedangkan bentuk JSON untuk snapshot idempotency ada di `service/task_snapshot.go`.
- **Interface didefinisikan di sisi konsumen** (`service/ports.go`, `handler/*`). Service dan handler bisa di-unit test dengan fake in-memory tanpa DB.
- **Transaksi lewat context.** `TxManager.WithinTx` menyimpan `pgx.Tx` di context; repository otomatis memakai transaksi itu. Service mengatur batas transaksi tanpa tahu detail SQL.
- **Waktu (`Clock`) dan lock (`KeyLocker`) di-inject** supaya TTL idempotency dan concurrency bisa diuji deterministik.
- **Migration di-embed** ke binary (`go:embed`) dan dijalankan saat start dalam satu transaksi dengan `pg_advisory_xact_lock`. Instance yang start bersamaan saling menunggu, dan migration yang gagal tidak meninggalkan skema setengah jadi.
- **Repository bergantung pada interface `postgres.DB`**, yang dipenuhi `*pgxpool.Pool`, sehingga repository, transaksi, dan migration bisa diuji dengan `pgxmock` tanpa database.

---

## Database Design

```
teams 1 ── * users 1 ── * tasks (owner_id)
                 1 ── * tasks (assignee_id, nullable)
tasks 1 ── * task_logs
users 1 ── * idempotency_keys
```

| Tabel | Kolom penting | Index / constraint |
|---|---|---|
| `teams` | `id`, `name`, `invite_code` | `UNIQUE (invite_code)`. Nama tidak unik |
| `users` | `id`, `team_id`, `name`, `email`, `password_hash` | `UNIQUE (lower(email))`, `(team_id)` |
| `tasks` | `id`, `owner_id`, `assignee_id`, `title`, `description`, `status`, `due_date` | `CHECK status`, `(owner_id, created_at DESC, id DESC)`, `(owner_id, status, created_at DESC)`, partial `(assignee_id, created_at DESC)`, GIN trigram `(title)` |
| `task_logs` | `task_id`, `actor_id`, `action`, `old_value` JSONB, `new_value` JSONB | `(task_id, created_at DESC)` |
| `idempotency_keys` | `user_id`, `idempotency_key`, `request_hash`, `result` JSONB (snapshot task), `expires_at` | `PRIMARY KEY (user_id, idempotency_key)`, `(expires_at)` |

Query efficiency:

- List task memakai **satu query** dengan `COUNT(*) OVER()` untuk total + data halaman. `COUNT` terpisah hanya dijalankan jika halaman melewati data terakhir.
- Search `ILIKE '%term%'` didukung index **GIN `pg_trgm`**, jadi tidak full table scan.
- Semua query memakai parameter (`$1`, `$2`, ...). Tidak ada string concatenation dari input user, sehingga aman dari SQL injection. Wildcard LIKE di-escape.
- Connection pool pgx dengan batas koneksi yang bisa dikonfigurasi.

---

## Implementasi Requirement

### 1. Idempotency (`POST /tasks`)

- Header `Idempotency-Key` (UUID) opsional. Jika ada tapi bukan UUID: `400 INVALID_IDEMPOTENCY_KEY`.
- Key di-scope per user (`PRIMARY KEY (user_id, idempotency_key)`), jadi user lain tidak bisa membaca response orang lain lewat key yang sama.
- Request pertama: `201` + task baru. Snapshot task disimpan bersama hash payload.
- Request ulang dalam 24 jam: `201` dengan body identik + header `Idempotent-Replayed: true`, tanpa membuat task baru.
- Key sama, payload berbeda: `422 IDEMPOTENCY_KEY_REUSED`.
- Setelah 24 jam key dianggap baru. Record kedaluwarsa di-reuse secara atomik, dan dibersihkan berkala oleh background job.

**Anti race condition, dua lapis:**

1. **Per-key lock in-process** (`pkg/keylock`). Request duplikat yang tiba bersamaan di instance yang sama diserialisasi: request kedua menunggu request pertama selesai lalu me-replay hasilnya.
2. **Klaim atomik di database.** Dalam satu transaksi: `INSERT ... ON CONFLICT DO UPDATE ... WHERE expired` (klaim key) → insert task → simpan response → commit. Transaksi lain dengan key yang sama (misalnya dari instance lain) **terblokir oleh row lock** sampai transaksi pertama selesai. Jika commit, ia melihat record dan me-replay. Jika rollback, ia mengambil alih key. Tidak ada jendela check-then-insert.

Tes e2e (`make e2e`): 30 request paralel dengan key yang sama menghasilkan 30 × `201` dengan body identik, dan **tepat 1 task** di database.

### 2. Structured Error Handling

- Satu tipe `apperror.Error` (kind, code, message, details, cause internal). Global error handler memetakan `kind` ke HTTP status.
- **Global error handler** Echo (`middleware/error_handler.go`) merender semua error dengan format yang sama, termasuk error bawaan Echo (404 route, 405, 413).
- **Recover middleware** menangkap panic: stack trace hanya ditulis ke log, client menerima `500 INTERNAL_SERVER_ERROR` generik.
- Panic di goroutine di luar request (HTTP server, cleanup job) ditangkap `pkg/concurrent.SafeGroup`: di-log dengan stack trace, lalu memicu graceful shutdown, bukan mematikan proses secara mendadak.
- 4xx membawa pesan yang aman untuk client. 5xx selalu memakai pesan generik. Penyebab internal hanya muncul sebagai field `debug` di `APP_ENV=development`, dan tidak pernah di production.
- Login memberi pesan sama untuk email tidak terdaftar dan password salah, serta tetap menjalankan bcrypt compare (mitigasi user enumeration dan timing attack).

### 3. Database Transaction & Integrity (`POST /tasks/:id/assign`)

Dalam **satu transaksi** (`TaskService.AssignTask`):

1. `SELECT ... FOR UPDATE` task (mengunci row, mencegah assign bersamaan saling menimpa), lalu cek owner.
2. Validasi assignee bukan owner, ada, dan **satu tim** dengan owner. Jika assignee sudah sama, selesai tanpa perubahan.
3. Update `assignee_id`.
4. Insert `task_logs` (`action=assigned`, `old_value`/`new_value` JSONB).
5. Kirim notifikasi. Implementasinya mock yang menulis notifikasi ke structured log ([internal/notifier](internal/notifier/notifier.go)), dengan `request_id` yang sama dengan request assign-nya:

   ```json
   {"level":"INFO","msg":"notification sent","request_id":"da9b5ce6-…","type":"task_assigned","task_id":"27450841-…","assignee_id":"30f709d1-…","assignee_email":"bob@example.com","actor_id":"0899b975-…"}
   ```

   Lihat dengan `make logs`, atau `docker logs task-management-api 2>&1 | grep '"notification sent"'`.

Jika langkah mana pun gagal, **seluruh transaksi di-rollback**. Untuk membuktikannya, `NOTIFIER_SIMULATE_FAILURE=true` membuat notifier selalu gagal (cara menyalakannya ada di [End-to-end Test](#end-to-end-test)). Hasilnya:
- assign dijawab `500`,
- `assignee_id` tidak berubah,
- `task_logs` tidak bertambah,
- di log tidak ada `notification sent`, yang muncul justru baris ERROR berikut:

```json
{"level":"ERROR","msg":"http request","request_id":"b78308cc-…","method":"POST","path":"/api/v1/tasks/52cf0f11-…/assign","status":500,"error_code":"INTERNAL_SERVER_ERROR","error":"… notify assignee: notifier: simulated failure"}
```

> Catatan: notifikasi dikirim di dalam transaksi sesuai soal. Di production, pola **transactional outbox** lebih tepat, supaya notifikasi tidak terkirim untuk transaksi yang ternyata gagal commit.

### 4. Logging & Observability

Setiap request menghasilkan satu log JSON (`log/slog`):

```json
{"time":"2026-10-13T01:03:45.17Z","level":"ERROR","msg":"http request","request_id":"04eb015f-62b1-4197-807b-7903f14cad99","method":"POST","path":"/api/v1/tasks/a566c81a-.../assign","route":"/api/v1/tasks/:id/assign","status":500,"latency_ms":6.987,"client_ip":"172.20.0.1","user_agent":"curl/8.7.1","bytes_out":239,"user_id":"465a6980-...","error_code":"INTERNAL_SERVER_ERROR","error":"..."}
```

- `request_id`: UUID baru yang dibuat server untuk setiap request, jadi selalu unik walaupun client mengirim `X-Request-ID` yang sama berulang kali. Nilainya dikembalikan di header `X-Request-ID` dan di body error.
- `X-Request-ID` dari client, kalau ada, dicatat terpisah sebagai `client_request_id` (maksimal 128 karakter) untuk korelasi dengan sistem pemanggil.
- Level: **INFO** untuk 2xx/3xx, **WARN** untuk 4xx, **ERROR** untuk 5xx.
- Logger ber-`request_id` disimpan di context, jadi log dari layer lain (notifier, panic) ikut membawa `request_id`.
- Semua log berformat JSON lewat `slog`, termasuk error level koneksi dari `net/http` (`http.Server.ErrorLog`).

### 5. Unit Testing

Semua test berjalan tanpa database maupun service eksternal:

- **Service** diuji dengan fake in-memory dari `internal/service/servicetest`. Fake ini meniru perilaku database yang diandalkan service. Transaksi yang gagal me-rollback semua perubahan. `IdempotencyRepository.Claim` dan `GetByIDForUpdate` menahan lock sampai transaksi selesai, seperti row lock PostgreSQL. Request duplikat yang bersamaan menunggu transaksi pertama, persis seperti di database sungguhan.
- **Repository** diuji dengan [`pgxmock`](https://github.com/pashagolub/pgxmock), driver PostgreSQL tiruan. Yang dicek: SQL, argumen, mapping hasil, transaksi, migration, dan semua jalur error.
- **Handler, middleware, dan router** diuji dengan `httptest` + Echo, termasuk format error, level log, panic, rate limit, dan JWT.

Skenario 5.1 (race condition idempotency):

| Skenario | Test |
|---|---|
| Sequential: request pertama `201`, request kedua dengan key sama tidak membuat task dan response-nya identik | `TestAPI_Idempotency_SequentialRetryReturnsIdenticalResponse` (lewat HTTP, body dibandingkan byte per byte), `TestCreateTask_Idempotency_RetryReturnsFirstResult` (service) |
| Concurrent duplicate: N goroutine dengan key sama, hasilnya tepat 1 task | `TestAPI_Idempotency_ConcurrentDuplicatesCreateOneTask` (50 request HTTP paralel), `TestCreateTask_Idempotency_ConcurrentDuplicatesCreateOneTask` (100 goroutine) |

Test concurrent di level service dijalankan dua kali. Varian pertama memakai lock in-process dan lock database bersamaan, seperti di production. Varian kedua hanya memakai lock database, seperti saat request duplikat masuk ke instance API yang berbeda. Kedua lapis pengaman dibuktikan masing-masing.

Test ini juga sudah diverifikasi bisa mendeteksi bug lewat mutation check. Row lock di fake dihilangkan, atau service dibuat mengabaikan hasil klaim key, dan test concurrent maupun sequential langsung gagal.

Concurrency lain yang diuji:
- update parsial bersamaan pada field berbeda tidak saling menimpa,
- assign bersamaan menghasilkan rantai `task_logs` yang konsisten,
- `keylock` hanya mengizinkan satu pemegang per key,
- key berbeda tidak saling menunggu.

Sisa 2% yang tidak ter-cover adalah jalur yang hanya bisa dicapai dengan database sungguhan, yaitu `app.Run` setelah koneksi berhasil dan `NewPool` yang sukses, ditambah `main()` dan cabang error library yang tidak mungkin terjadi, misalnya `json.Marshal` pada struct snapshot.

### Security basic practice

- Password di-hash dengan bcrypt (cost 12). Batas 72 **byte** (bukan karakter) divalidasi di request. Password yang lebih panjang juga ditolak saat login, karena bcrypt diam-diam hanya membaca 72 byte pertama.
- Rate limit `/auth/register` dan `/auth/login`: default 60 request per menit per IP (`AUTH_RATE_LIMIT`), lalu `429 RATE_LIMIT_EXCEEDED`. IP diambil dari koneksi TCP, bukan dari header `X-Forwarded-For` yang bisa dipalsukan. Counter disimpan di memory, jadi berlaku per instance.
- Join tim hanya lewat invite code acak (122 bit). Nama tim tidak bisa dipakai untuk masuk ke tim orang lain.
- JWT HS256 dengan validasi algoritma, issuer, dan `exp` wajib. Secret minimal 32 karakter, divalidasi saat start.
- Otorisasi per resource (owner/assignee). Akses ke task orang lain dijawab 404.
- Validasi input di semua endpoint. Teks di-trim sebelum divalidasi, jadi nilai berisi spasi saja ditolak. Teks berisi karakter NUL atau UTF-8 yang tidak valid ditolak `400`, karena PostgreSQL tidak bisa menyimpannya; tanpa aturan ini input tersebut akan muncul sebagai `500`. Body limit 1 MB, secure headers (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`).
- HTTP server dengan read/write/idle timeout. Graceful shutdown.
- Container berjalan sebagai non-root di image distroless.

---

## Perintah Makefile

```
make help              daftar target
make env               buat .env dari .env.example (JWT_SECRET acak) jika belum ada
make up                API + PostgreSQL bawaan (Docker)
make up-external-db    API (Docker) + PostgreSQL yang sudah ada
make down              stop container
make down-volumes      stop + hapus data PostgreSQL bawaan
make logs              follow log API
make ps                status container
make db-up             hanya PostgreSQL bawaan
make dev               PostgreSQL bawaan + API di host
make run               API di host (pakai .env)
make build             build binary ke ./bin
make docker-build      build image Docker API
make test              unit test
make test-race         unit test + race detector
make test-idempotency  test idempotency & concurrency (race detector)
make e2e               end-to-end test ke API yang sedang jalan
make cover             coverage per package, gagal jika total < 95%
make vet / fmt / tidy  quality tools
make clean             hapus binary dan artefak coverage
```
