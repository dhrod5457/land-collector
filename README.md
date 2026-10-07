# land-collector

국토교통부/공공데이터포털 계열 토지 OpenAPI를 지역별로 병렬 수집하고 스케줄링하는 경량 Go 서비스입니다.

## 주요 기능

- 토지·임야정보, 토지특성정보, 개별공시지가, 토지이용계획정보 수집
- PNU 지역코드 prefix 기반 지역 선택 수집
- 시도 및 제주 시군구 선택
- 즉시 수집
- 매일 / 매주 / 매월 스케줄
- 동일 지역 중복 실행 방지
- API별 독립 RPS 제한
- 429 / 5xx / 네트워크 오류 재시도
- 실패 요청 영속화
- 실행 이력 및 실패 현황 관리
- Tabler 기반 관리자 UI
- SQLite 단일 파일 저장

## 구성

```text
Browser
  |
Go container
  +-- Admin UI
  +-- Scheduler
  +-- Collector workers
  +-- SQLite /data/land-collector.db
```

PostgreSQL, Redis, Node.js 런타임 없이 단일 컨테이너로 동작합니다.

## 실행

```sh
cp .env.example .env
docker compose up --build
```

관리 화면:

```text
http://localhost:8080/admin
```

관리 화면은 HTTP Basic Auth로 보호합니다. `.env`의 `ADMIN_PASSWORD`는 반드시 변경하세요.

## 데이터

SQLite 파일은 Docker volume의 다음 위치에 저장합니다.

```text
/data/land-collector.db
```

PNU 입력 파일은 다음 경로를 사용합니다.

```text
/data/pnu.txt
```

현재 지역 수집은 이 파일에서 선택한 지역코드로 시작하는 PNU만 골라 처리합니다.

예:

- 제주특별자치도: `50`
- 제주시: `50110`
- 서귀포시: `50130`

## SQLite 운영

시작 시 아래 설정을 자동 적용합니다.

- WAL
- synchronous=NORMAL
- foreign_keys=ON
- busy_timeout=5000

DB 연결 수는 1개로 제한해 SQLite 쓰기 경합과 메모리 사용을 최소화합니다. HTTP OpenAPI 호출의 병렬성은 별도로 유지됩니다.

## 관리자 UI

Tabler 기반 서버 렌더링 UI를 사용합니다.

- 대시보드
- 즉시 수집
- 스케줄 관리
- 실행 이력
- 실패 관리

프론트엔드 SPA나 Node.js 빌드는 사용하지 않습니다.

## OpenAPI 설정

공공데이터포털의 대상 서비스는 LINK 유형이므로 활용신청 후 받은 실제 URL을 template으로 설정합니다.

```text
...?authKey={serviceKey}&pnu={pnu}&format=json
```

각 URL에는 반드시 `{serviceKey}`, `{pnu}`가 포함되어야 합니다.

## 자원 목표

기본 Docker 제한:

- CPU 1 core
- Memory 256MB

## 테스트

```sh
go test ./...
```

Pull Request와 main push 시 GitHub Actions에서도 전체 테스트를 실행합니다.
