CREATE TABLE IF NOT EXISTS land_records (
    pnu         varchar(19) NOT NULL,
    dataset     varchar(32) NOT NULL,
    observed_at timestamptz NOT NULL,
    payload     jsonb NOT NULL,
    PRIMARY KEY (pnu, dataset)
);

CREATE INDEX IF NOT EXISTS idx_land_records_dataset_observed_at
    ON land_records(dataset, observed_at DESC);
