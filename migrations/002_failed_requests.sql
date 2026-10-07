CREATE TABLE IF NOT EXISTS failed_requests (
    pnu         varchar(19) NOT NULL,
    dataset     varchar(32) NOT NULL,
    failed_at   timestamptz NOT NULL,
    error_text  text NOT NULL,
    attempts    integer NOT NULL DEFAULT 1,
    PRIMARY KEY (pnu, dataset)
);

CREATE INDEX IF NOT EXISTS idx_failed_requests_failed_at
    ON failed_requests(failed_at);
