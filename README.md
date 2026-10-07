# land-collector

국토교통부/공공데이터포털 계열 토지 OpenAPI를 PNU 기준으로 병렬 수집하는 경량 Go 수집기입니다.

## 대상 데이터

- 토지·임야정보
- 토지특성정보
- 개별공시지가
- 토지이용계획정보

공공데이터포털의 해당 서비스는 LINK 유형입니다. 활용신청 후 제공되는 실제 호출 URL을 환경변수에 넣습니다. URL을 코드에 고정하지 않아 제공기관의 엔드포인트/인증 파라미터 변경을 수집 엔진과 분리합니다.

각 URL은 아래 두 placeholder를 사용할 수 있습니다.

- `{serviceKey}`: 발급된 서비스키
- `{pnu}`: 19자리 PNU

예:

```text
https://provider.example/api?authKey={serviceKey}&pnu={pnu}&format=json
```

## 실행

1. `.env.example`을 `.env`로 복사합니다.
2. 서비스키와 4개 실제 API URL template을 설정합니다.
3. `data/pnu.txt`에 한 줄에 하나씩 19자리 PNU를 넣습니다.
4. `docker compose up --build`를 실행합니다.

## 주요 설정

- `WORKERS`: PNU 병렬 처리 worker 수
- `REQUESTS_PER_SECOND`: API 종류별 요청 rate limit
- `BATCH_SIZE`: DB 저장 batch 크기
- `HTTP_TIMEOUT_SECONDS`: HTTP timeout
- `MAX_RETRIES`: 429/5xx/network 오류 재시도 횟수

4개 API는 각각 독립된 rate limiter를 가집니다. 따라서 `REQUESTS_PER_SECOND=10`이면 이론상 API별 최대 10 req/s이고, 4종 전체는 최대 약 40 req/s까지 진행될 수 있습니다. 실제 값은 활용신청으로 부여된 트래픽 정책보다 낮게 설정해야 합니다.

기본 컨테이너 제한은 1 CPU / 256MB입니다.

## 오류 처리

- 입력 PNU는 정확히 19자리 숫자인지 수집 전에 검증합니다.
- HTTP 429 및 5xx, 네트워크 오류는 exponential backoff로 재시도합니다.
- 정상 HTTP 응답이어도 JSON이 아니면 저장하지 않습니다.
- 서비스키는 환경변수로만 주입하며 로그에 URL이나 키를 출력하지 않습니다.

## 데이터 저장

현재는 API 원문 JSON을 PostgreSQL `jsonb`로 보존합니다. PNU와 dataset을 복합 PK로 사용해 재수집 시 최신 원문으로 upsert합니다.

공시지가의 연도별 이력과 각 데이터셋의 정규화 컬럼은 실제 활용신청 후 확인되는 API 응답 명세를 기준으로 별도 migration으로 확장할 수 있습니다.

## 테스트

```sh
go test ./...
```

수집/병렬 처리 핵심 패키지는 외부 서비스 없이 단위 테스트할 수 있습니다.
