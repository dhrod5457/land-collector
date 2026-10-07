# land-collector

국토교통부/공공데이터포털 계열 토지 OpenAPI를 PNU 기준으로 병렬 수집하는 경량 Go 수집기입니다.

## 대상 데이터

- 토지·임야정보
- 토지특성정보
- 개별공시지가
- 토지이용계획정보

공공데이터포털의 4개 대상 서비스는 LINK 유형입니다. 활용신청 후 제공되는 실제 호출 URL을 환경변수로 넣습니다.

각 URL은 `{serviceKey}`와 `{pnu}` placeholder를 포함해야 합니다.

```text
https://provider.example/api?authKey={serviceKey}&pnu={pnu}&format=json
```

## 실행

1. `.env.example`을 `.env`로 복사합니다.
2. 서비스키와 4개 실제 API URL template을 설정합니다.
3. `data/pnu.txt`에 한 줄에 하나씩 19자리 PNU를 넣습니다.
4. `docker compose up --build`를 실행합니다.

## 병렬 수집

- `WORKERS`: 동시에 처리할 PNU worker 수
- `LAND_RPS`: 토지·임야 API 초당 요청 수
- `CHARACTERISTIC_RPS`: 토지특성 API 초당 요청 수
- `PRICE_RPS`: 개별공시지가 API 초당 요청 수
- `USE_PLAN_RPS`: 토지이용계획 API 초당 요청 수
- `REQUESTS_PER_SECOND`: 위 개별 설정이 없을 때 사용하는 기본값
- `BATCH_SIZE`: DB 저장 batch 크기

API 종류마다 독립된 rate limiter를 사용합니다. 실제 값은 활용신청으로 승인된 트래픽보다 낮게 설정해야 합니다.

기본 Docker 제한은 CPU 1 / memory 256MB입니다.

## 실패 복구

- HTTP 429, 5xx, 네트워크 오류는 exponential backoff로 재시도합니다.
- 재시도 후에도 실패한 PNU/API 조합은 `failed_requests`에 저장합니다.
- 같은 PNU/API가 이후 정상 수집되면 해당 실패 기록은 자동 삭제합니다.
- `attempts` 컬럼으로 반복 실패 건을 구분할 수 있습니다.
- 잘못된 PNU는 API 호출 전에 차단합니다.

## 데이터 저장

정상 응답 원문은 PostgreSQL `jsonb`로 저장합니다.

```text
land_records
  PK: pnu + dataset
  observed_at
  payload jsonb

failed_requests
  PK: pnu + dataset
  failed_at
  attempts
  error_text
```

공시지가의 연도별 이력과 각 데이터셋의 업무 컬럼 정규화는 활용신청 후 확인되는 실제 응답 명세를 기준으로 확장합니다. 공개 카탈로그만으로는 LINK API의 세부 필드 계약을 확정하지 않습니다.

## 테스트

```sh
go test ./...
```

Pull Request와 main push 시 GitHub Actions에서도 전체 테스트를 실행합니다.
