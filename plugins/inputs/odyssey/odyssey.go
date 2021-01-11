package odyssey

import (
	"bytes"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/postgresql"
	_ "github.com/jackc/pgx/stdlib" // register driver
)

type Odyssey struct {
	postgresql.Service
}

var ignoredColumns = map[string]bool{"user": true, "database": true, "pool_mode": true,
	"avg_req": true, "avg_recv": true, "avg_sent": true, "avg_query": true,
}

var sampleConfig = `
  ## specify address via a url matching:
  ##   postgres://[pqgotest[:password]]@localhost[/dbname]\
  ##       ?sslmode=[disable|verify-ca|verify-full]
  ## or a simple string:
  ##   host=localhost user=console password=... sslmode=... dbname=app_production
  ##
  ## All connection parameters are optional.
  ##
  address = "host=localhost user=postgres sslmode=disable"
`

func (p *Odyssey) SampleConfig() string {
	return sampleConfig
}

func (p *Odyssey) Description() string {
	return "Read metrics from Odyssey"
}

func (p *Odyssey) Gather(acc telegraf.Accumulator) error {
	var (
		err     error
		query   string
		columns []string
	)

	query = `SHOW STATS`

	rows, err := p.DB.Query(query)
	if err != nil {
		return err
	}

	defer rows.Close()

	// grab the column information from the result
	if columns, err = rows.Columns(); err != nil {
		return err
	}

	for rows.Next() {
		tags, columnMap, err := p.accRow(rows, acc, columns)

		if err != nil {
			return err
		}

		fields := make(map[string]interface{})
		for col, val := range columnMap {
			_, ignore := ignoredColumns[col]
			if ignore {
				continue
			}

			switch v := (*val).(type) {
			case int64:
				fields[col] = v
			case string:
				integer, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return err
				}

				fields[col] = integer
			}
		}
		acc.AddFields("odyssey", fields, tags)
	}

	err = rows.Err()
	if err != nil {
		return err
	}

	query = `SHOW POOLS`

	poolRows, err := p.DB.Query(query)
	if err != nil {
		return err
	}

	defer poolRows.Close()

	// grab the column information from the result
	if columns, err = poolRows.Columns(); err != nil {
		return err
	}

	for poolRows.Next() {
		tags, columnMap, err := p.accRow(poolRows, acc, columns)
		if err != nil {
			return err
		}

		if user, ok := columnMap["user"]; ok {
			if s, ok := (*user).(string); ok && s != "" {
				tags["user"] = s
			}
		}

		if poolMode, ok := columnMap["pool_mode"]; ok {
			if s, ok := (*poolMode).(string); ok && s != "" {
				tags["pool_mode"] = s
			}
		}

		fields := make(map[string]interface{})
		for col, val := range columnMap {
			_, ignore := ignoredColumns[col]
			if !ignore {
				fields[col] = *val
			}
		}
		acc.AddFields("odyssey_pools", fields, tags)
	}

	return poolRows.Err()
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func (p *Odyssey) accRow(row scanner, acc telegraf.Accumulator, columns []string) (map[string]string,
	map[string]*interface{}, error) {
	var columnVars []interface{}
	var dbname bytes.Buffer

	// this is where we'll store the column name with its *interface{}
	columnMap := make(map[string]*interface{})

	for _, column := range columns {
		columnMap[column] = new(interface{})
	}

	// populate the array of interface{} with the pointers in the right order
	for i := 0; i < len(columnMap); i++ {
		columnVars = append(columnVars, columnMap[columns[i]])
	}

	// deconstruct array of variables and send to Scan
	err := row.Scan(columnVars...)

	if err != nil {
		return nil, nil, err
	}
	if columnMap["database"] != nil {
		// extract the database name from the column map
		dbname.WriteString((*columnMap["database"]).(string))
	} else {
		dbname.WriteString("postgres")
	}

	var tagAddress string
	tagAddress, err = p.SanitizedAddress()
	if err != nil {
		return nil, nil, err
	}

	// Return basic tags and the mapped columns
	return map[string]string{"server": tagAddress, "db": dbname.String()}, columnMap, nil
}

func init() {
	inputs.Add("odyssey", func() telegraf.Input {
		return &Odyssey{
			Service: postgresql.Service{
				MaxIdle: 1,
				MaxOpen: 1,
				MaxLifetime: internal.Duration{
					Duration: 0,
				},
			},
		}
	})
}
