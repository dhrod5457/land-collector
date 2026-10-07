CREATE TABLE IF NOT EXISTS regions (
    code        varchar(10) PRIMARY KEY,
    name        varchar(100) NOT NULL,
    level       varchar(20) NOT NULL,
    parent_code varchar(10)
);

INSERT INTO regions (code, name, level, parent_code) VALUES
('11','서울특별시','sido',NULL),
('26','부산광역시','sido',NULL),
('27','대구광역시','sido',NULL),
('28','인천광역시','sido',NULL),
('29','광주광역시','sido',NULL),
('30','대전광역시','sido',NULL),
('31','울산광역시','sido',NULL),
('36','세종특별자치시','sido',NULL),
('41','경기도','sido',NULL),
('43','충청북도','sido',NULL),
('44','충청남도','sido',NULL),
('46','전라남도','sido',NULL),
('47','경상북도','sido',NULL),
('48','경상남도','sido',NULL),
('50','제주특별자치도','sido',NULL),
('51','강원특별자치도','sido',NULL),
('52','전북특별자치도','sido',NULL),
('50110','제주시','sigungu','50'),
('50130','서귀포시','sigungu','50')
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS collection_schedules (
    id           bigserial PRIMARY KEY,
    name         varchar(120) NOT NULL,
    region_code  varchar(10) NOT NULL REFERENCES regions(code),
    frequency    varchar(10) NOT NULL CHECK (frequency IN ('daily','weekly','monthly')),
    hour         smallint NOT NULL CHECK (hour BETWEEN 0 AND 23),
    minute       smallint NOT NULL CHECK (minute BETWEEN 0 AND 59),
    weekday      smallint CHECK (weekday BETWEEN 0 AND 6),
    day_of_month smallint CHECK (day_of_month BETWEEN 1 AND 31),
    datasets     text NOT NULL DEFAULT 'land,characteristic,price,use_plan',
    enabled      boolean NOT NULL DEFAULT true,
    last_run_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS collection_runs (
    id           bigserial PRIMARY KEY,
    schedule_id  bigint REFERENCES collection_schedules(id) ON DELETE SET NULL,
    region_code  varchar(10) NOT NULL,
    region_name  varchar(100) NOT NULL,
    trigger_type varchar(20) NOT NULL CHECK (trigger_type IN ('manual','scheduled')),
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    status       varchar(24) NOT NULL,
    message      text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_collection_schedules_enabled
    ON collection_schedules(enabled);
CREATE INDEX IF NOT EXISTS idx_collection_runs_started_at
    ON collection_runs(started_at DESC);
