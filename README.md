# land-collector

국토교통부/공공데이터포털 계열 토지 OpenAPI를 PNU 기준으로 병렬 수집하는 경량 Go 수집기입니다.

## 대상 데이터

- 토지·임야정보
- 토지특성정보
- 개별공시지가
- 토지이용계획정보

공공데이터포털의 해당 서비스는 LINK 유형이므로 활용신청 후 발급받은 실제 호출 URL을 환경변수에 넣습니다. URL을 코드에 고정하지 않아 제공기관의 엔드포인트 변경을 수집 엔진과 분리합니다.

## 실행

1. `.env.example`을 `.env`로 복사합니다.
2. 서비스키와 4개 API URL을 설정합니다.
3. `data/pnu.txt`에 한 줄에 하나씩 19자리 PNU를 넣습니다.
4. `docker compose up --build`를 실행합니다.

## 주요 설정

- `WORKERS`: PNU 병렬 처리 worker 수
- `REQUESTS_PER_SECOND`: 전체 API 요청 rate limit
- `BATCH_SIZE`: DB 저장 batch 크기
- `HTTP_TIMEOUT_SECONDS`: HTTP timeout
- `MAX_RETRIES`: 429/5xx/network 오류 재시도 횟수

기본값은 1 CPU / 256MB 컨테이너에서 시작하도록 보수적으로 잡았습니다. 실제 허용 트래픽에 맞춰 `REQUESTS_PER_SECOND`를 먼저 조정한 뒤 worker 수를 늘리는 것을 권장합니다.

## 데이터 저장

현재는 API 원문 JSON을 `jsonb`로 보존합니다. PNU와 dataset을 복합 PK로 사용해 재수집 시 최신 원문으로 upsert합니다. 공시지가의 연도별 이력 등 정규화 스키마는 실제 API 응답 명세와 운영 요구를 확정한 뒤 별도 migration으로 확장할 수 있습니다.

## 테스트

```sh
go test ./...
```
