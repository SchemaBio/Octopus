package model

type ParquetFilter struct {
	Column   string      `json:"column" binding:"required"`
	Operator string      `json:"operator" binding:"required"`
	Value    interface{} `json:"value"`
}

type ParquetQueryRequest struct {
	Offset    int64           `json:"offset"`
	Limit     int64           `json:"limit"`
	Search    string          `json:"search"`
	Sort      string          `json:"sort"`
	Direction string          `json:"direction"`
	Filters   []ParquetFilter `json:"filters"`
}

type ParquetQueryResponse struct {
	Items               []map[string]interface{} `json:"items"`
	Total               int64                    `json:"total"`
	RowCount            int64                    `json:"rowCount"`
	Offset              int64                    `json:"offset"`
	Limit               int64                    `json:"limit"`
	Columns             []string                 `json:"columns"`
	ColumnTypes         map[string]string        `json:"columnTypes"`
	Version             string                   `json:"version"`
	FieldProfileVersion string                   `json:"fieldProfileVersion"`
}
