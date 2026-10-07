package domain

import "time"

type Dataset string

const (
	DatasetLand           Dataset = "land"
	DatasetCharacteristic Dataset = "characteristic"
	DatasetPrice          Dataset = "price"
	DatasetUsePlan        Dataset = "use_plan"
)

type Record struct {
	PNU         string
	Dataset     Dataset
	ObservedAt  time.Time
	PayloadJSON []byte
}
