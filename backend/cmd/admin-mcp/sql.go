package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"islandBattles/lobby/adminmcp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The query tool's caps on its result. A longer value is cut to
// maxValueBytes before it is converted and encoded, but the driver still
// reads each whole row into memory first, up to PostgreSQL's 1 GB limit on a
// value, within the statement timeout.
const (
	maxQueryRows  = 200
	maxQueryBytes = 64 << 10
	maxValueBytes = 16 << 10
)

// secretColumns are left out of query results by name. The production
// read-only role can't read them at all; this keeps a `SELECT *` by the local
// development role, which owns every table, from returning them.
var secretColumns = []string{"bearer_hash"}

// systemSchemas hold PostgreSQL's own catalogs, which describe_schema leaves
// out.
var systemSchemas = []any{"pg_catalog", "information_schema"}

// informationSchemaTable is a row of information_schema.tables.
type informationSchemaTable struct {
	Schema string `gorm:"column:table_schema"`
	Name   string `gorm:"column:table_name"`
	Type   string `gorm:"column:table_type"`
}

func (informationSchemaTable) TableName() string { return "information_schema.tables" }

// informationSchemaColumn is a row of information_schema.columns.
type informationSchemaColumn struct {
	Schema   string  `gorm:"column:table_schema"`
	Table    string  `gorm:"column:table_name"`
	Name     string  `gorm:"column:column_name"`
	DataType string  `gorm:"column:data_type"`
	UDTName  string  `gorm:"column:udt_name"`
	Nullable string  `gorm:"column:is_nullable"`
	Default  *string `gorm:"column:column_default"`
}

func (informationSchemaColumn) TableName() string { return "information_schema.columns" }

// The databases that the SQL tools read: Island Battles' and the lobby's,
// which are two databases since #229, so no statement reads both.
const (
	GameDatabase  = "game"
	LobbyDatabase = "lobby"
)

// databaseDescription describes the SQL tools' database argument.
const databaseDescription = "The database to read: game, Island Battles' games, generations, maps, castles, soldier groups and commands; " +
	"or lobby, the lobby's lobby_* tables of lobbies, teams, members and players. They are two databases, so no statement reads both."

// sqlDatabases are the databases that the SQL tools read, by name.
type sqlDatabases map[string]*gorm.DB

// pick returns the database that name names.
func (databases sqlDatabases) pick(name string) (*gorm.DB, error) {
	database, found := databases[name]
	if !found {
		return nil, fmt.Errorf("database must be %s or %s, not %q", GameDatabase, LobbyDatabase, name)
	}
	return database, nil
}

// DescribeSchemaInput names the database, and the table to describe.
type DescribeSchemaInput struct {
	Database string `json:"database" jsonschema:"The database to describe: game, Island Battles', or lobby, the lobby's."`
	Table    string `json:"table,omitempty" jsonschema:"A table to list the columns of. Without it, the tables are listed."`
}

// SchemaTable is a table or view.
type SchemaTable struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type" jsonschema:"BASE TABLE or VIEW."`
}

// SchemaColumn is one column of a table.
type SchemaColumn struct {
	Schema   string  `json:"schema"`
	Table    string  `json:"table"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
}

// Schema is describe_schema's answer: the tables, or one table's columns.
type Schema struct {
	Tables  []SchemaTable  `json:"tables,omitempty"`
	Columns []SchemaColumn `json:"columns,omitempty"`
}

// QueryInput is one SQL statement, and the database to run it in.
type QueryInput struct {
	Database string `json:"database" jsonschema:"The database to read: game, Island Battles', or lobby, the lobby's."`
	SQL      string `json:"sql" jsonschema:"One SELECT, VALUES, TABLE or WITH statement."`
}

// QueryColumn names a result column and its PostgreSQL type.
type QueryColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// QueryResult is query's answer.
type QueryResult struct {
	Columns   []QueryColumn `json:"columns"`
	Rows      [][]any       `json:"rows" jsonschema:"One array of values per row, in the order of columns."`
	Truncated bool          `json:"truncated" jsonschema:"Whether rows or values were cut to fit the caps."`
	Notes     []string      `json:"notes,omitempty"`
}

// addSQLTools registers describe_schema and query, which read the database
// that their database argument names.
func addSQLTools(server *mcp.Server, databases sqlDatabases, logger *slog.Logger) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "describe_schema",
		Description: "Lists a database's tables, or one table's columns with their types, nullability and defaults, from information_schema, " +
			"to write SQL for query. " + databaseDescription,
		Annotations: adminmcp.ReadOnlyAnnotations("Describe schema"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DescribeSchemaInput) (*mcp.CallToolResult, Schema, error) {
		database, err := databases.pick(input.Database)
		if err != nil {
			return nil, Schema{}, err
		}
		var schema Schema
		err = adminmcp.ReadOnly(ctx, database, func(transaction *gorm.DB) error {
			var err error
			schema, err = describeSchema(transaction, input.Table)
			return err
		})
		return nil, schema, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "query",
		Description: fmt.Sprintf("Runs one read-only SQL statement, a SELECT, VALUES, TABLE or WITH query, for questions the other tools don't answer. "+
			"Use describe_schema first. Several statements are rejected, and so is anything that writes. The statement times out after %s. "+
			"The result is cut to %d rows and %d KiB, and truncated says when. Times are returned in UTC. %s",
			adminmcp.StatementTimeout, maxQueryRows, maxQueryBytes>>10, databaseDescription),
		Annotations: adminmcp.ReadOnlyAnnotations("Query"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input QueryInput) (*mcp.CallToolResult, QueryResult, error) {
		database, err := databases.pick(input.Database)
		if err != nil {
			return nil, QueryResult{}, err
		}
		var result QueryResult
		err = adminmcp.ReadOnly(ctx, database, func(transaction *gorm.DB) error {
			if err := adminmcp.CheckCredentialsHidden(transaction); err != nil {
				err = credentialsError(err)
				logger.Error("query is refused", "database", input.Database, "error", err)
				return err
			}
			var err error
			result, err = runQuery(transaction, input.SQL)
			return err
		})
		return nil, result, err
	})
}

func describeSchema(transaction *gorm.DB, table string) (Schema, error) {
	notSystem := clause.Not(clause.IN{Column: clause.Column{Name: "table_schema"}, Values: systemSchemas})
	if table == "" {
		var tables []informationSchemaTable
		if err := transaction.Where(notSystem).
			Order(clause.OrderByColumn{Column: clause.Column{Name: "table_schema"}}).Order(clause.OrderByColumn{Column: clause.Column{Name: "table_name"}}).
			Find(&tables).Error; err != nil {
			return Schema{}, fmt.Errorf("list tables: %w", err)
		}
		schema := Schema{Tables: make([]SchemaTable, 0, len(tables))}
		for _, table := range tables {
			schema.Tables = append(schema.Tables, SchemaTable(table))
		}
		return schema, nil
	}
	var columns []informationSchemaColumn
	if err := transaction.Where(notSystem).Where(&informationSchemaColumn{Table: table}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "table_schema"}}).Order(clause.OrderByColumn{Column: clause.Column{Name: "ordinal_position"}}).
		Find(&columns).Error; err != nil {
		return Schema{}, fmt.Errorf("list columns: %w", err)
	}
	if len(columns) == 0 {
		return Schema{}, fmt.Errorf("no table %q, or none of its columns is readable; call describe_schema without a table to list them", table)
	}
	schema := Schema{Columns: make([]SchemaColumn, 0, len(columns))}
	for _, column := range columns {
		columnType := column.DataType
		// Arrays and enums say only ARRAY or USER-DEFINED; the type's own
		// name says more.
		if columnType == "ARRAY" || columnType == "USER-DEFINED" {
			columnType = column.UDTName
		}
		schema.Columns = append(schema.Columns, SchemaColumn{
			Schema: column.Schema, Table: column.Table, Name: column.Name, Type: columnType, Nullable: column.Nullable == "YES", Default: column.Default,
		})
	}
	return schema, nil
}

// credentialsError says how to fix a role that can read bearer hashes.
func credentialsError(err error) error {
	switch {
	case errors.Is(err, adminmcp.ErrCredentialsReadable):
		return fmt.Errorf("%w. Run the VM deploy, which grants them again: see \"The admin MCP server\" in deploy/README.md", err)
	case errors.Is(err, adminmcp.ErrCredentialsColumnChanged):
		return fmt.Errorf("%w. Update the lobby_sessions column grants in deploy/ansible/roles/app/templates/admin-grants-lobby.sql.j2 and run the VM deploy", err)
	}
	return err
}

// cursorName names the cursor that runQuery reads a statement through.
const cursorName = "admin_query"

// runQuery runs statement through a cursor and fetches one row more than the
// cap, so it learns whether rows were left out without reading them all.
// DECLARE takes only a query, so the statement can't be a command such as
// SET or COPY; the read-only transaction refuses anything in it that writes.
// The read-only transaction alone wouldn't do: SET TRANSACTION READ WRITE
// as its first statement makes it read-write, and it still lets a
// superuser, such as the local development role, run COPY ... TO PROGRAM.
//
// Both statements use the extended protocol without caching, so PostgreSQL
// rejects a statement that holds several, and each FETCH is described anew.
// The statement is sent as it is, so its own comments and placeholders are
// PostgreSQL's to read.
func runQuery(transaction *gorm.DB, statement string) (QueryResult, error) {
	if strings.TrimSpace(statement) == "" {
		return QueryResult{}, errors.New("sql is empty")
	}
	// The transaction's context carries the tool's timeout.
	ctx := transaction.Statement.Context
	sqlTransaction, err := adminmcp.SQLTransaction(transaction)
	if err != nil {
		return QueryResult{}, err
	}
	declared, err := sqlTransaction.QueryContext(ctx, "DECLARE "+cursorName+" NO SCROLL CURSOR FOR "+statement, pgx.QueryExecModeDescribeExec)
	if err != nil {
		return QueryResult{}, explainQueryError(err)
	}
	if err := declared.Close(); err != nil {
		return QueryResult{}, explainQueryError(err)
	}
	rows, err := sqlTransaction.QueryContext(ctx, fmt.Sprintf("FETCH %d FROM %s", maxQueryRows+1, cursorName), pgx.QueryExecModeDescribeExec)
	if err != nil {
		return QueryResult{}, explainQueryError(err)
	}
	defer func() { _ = rows.Close() }()
	result, err := readRows(rows)
	if err != nil {
		return QueryResult{}, explainQueryError(err)
	}
	return result, nil
}

// readRows reads at most maxQueryRows rows and maxQueryBytes of them, and
// leaves out secret columns.
func readRows(rows *sql.Rows) (QueryResult, error) {
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return QueryResult{}, err
	}
	result := QueryResult{Columns: []QueryColumn{}, Rows: [][]any{}}
	var kept []int
	for index, columnType := range columnTypes {
		if slices.Contains(secretColumns, strings.ToLower(columnType.Name())) {
			result.Notes = append(result.Notes, "Left out a column that holds credentials.")
			continue
		}
		kept = append(kept, index)
		result.Columns = append(result.Columns, QueryColumn{Name: columnType.Name(), Type: strings.ToLower(columnType.DatabaseTypeName())})
	}
	values := make([]any, len(columnTypes))
	pointers := make([]any, len(columnTypes))
	for index := range values {
		pointers[index] = &values[index]
	}
	size := 0
	cutValues := 0
	for rows.Next() {
		if len(result.Rows) == maxQueryRows {
			result.Truncated = true
			result.Notes = append(result.Notes, fmt.Sprintf("Cut to the first %d rows. Narrow the query or aggregate it to see the rest.", maxQueryRows))
			break
		}
		if err := rows.Scan(pointers...); err != nil {
			return QueryResult{}, err
		}
		row := make([]any, 0, len(kept))
		for _, index := range kept {
			value, cut := cutValue(values[index], columnTypes[index].DatabaseTypeName())
			if cut {
				cutValues++
			}
			row = append(row, jsonValue(value, columnTypes[index].DatabaseTypeName()))
		}
		encoded, err := encodeRow(row)
		if err != nil {
			return QueryResult{}, err
		}
		if size+len(encoded) > maxQueryBytes {
			result.Truncated = true
			result.Notes = append(result.Notes, fmt.Sprintf("Cut after %d rows to stay within %d KiB. Select fewer or shorter columns to see more.", len(result.Rows), maxQueryBytes>>10))
			break
		}
		size += len(encoded) + 1
		result.Rows = append(result.Rows, row)
	}
	if cutValues > 0 {
		result.Truncated = true
		result.Notes = append(result.Notes, fmt.Sprintf("Cut values longer than %d KiB to their first %d KiB (%d of them). Select a part of them, such as with left(), to see the rest.", maxValueBytes>>10, maxValueBytes>>10, cutValues))
	}
	return result, rows.Err()
}

// encodeRow encodes a row as the size caps count it. HTML escaping is off,
// so a value full of <, > or & isn't counted six times its length.
func encodeRow(row []any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(row); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n")), nil
}

// cutValue cuts text and bytes longer than maxValueBytes, before they are
// converted or encoded, and reports whether it cut. Cut text stays valid
// UTF-8, also the json, jsonb and xml values that pgx returns as bytes, and
// cut JSON is returned as text.
func cutValue(value any, databaseType string) (any, bool) {
	switch typed := value.(type) {
	case string:
		if len(typed) > maxValueBytes {
			return strings.ToValidUTF8(typed[:maxValueBytes], ""), true
		}
	case []byte:
		if len(typed) <= maxValueBytes {
			return value, false
		}
		cut := typed[:maxValueBytes]
		if databaseType != "BYTEA" {
			// A character is at most 4 bytes, so a cut inside one leaves at
			// most 3 of its bytes to drop.
			for dropped := 0; dropped < utf8.UTFMax-1 && !utf8.Valid(cut); dropped++ {
				cut = cut[:len(cut)-1]
			}
		}
		return cut, true
	}
	return value, false
}

// jsonValue turns a value that pgx scanned into one that JSON represents
// faithfully: times in UTC, JSON columns as JSON, bytes as PostgreSQL's hex
// text, and floats that JSON lacks as text.
func jsonValue(value any, databaseType string) any {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC()
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Sprint(typed)
		}
		return typed
	case float32:
		return jsonValue(float64(typed), databaseType)
	case []byte:
		if databaseType == "BYTEA" || !utf8.Valid(typed) {
			return `\x` + hex.EncodeToString(typed)
		}
		return jsonValue(string(typed), databaseType)
	case string:
		if (databaseType == "JSON" || databaseType == "JSONB") && json.Valid([]byte(typed)) {
			return json.RawMessage(typed)
		}
		return typed
	default:
		return typed
	}
}

// The SQLSTATE codes that explainQueryError explains.
const (
	syntaxError            = "42601"
	readOnlySQLTransaction = "25006"
	queryCanceled          = "57014"
	insufficientPrivilege  = "42501"
)

// explainQueryError says what an operator can do about a failed statement.
func explainQueryError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return err
	}
	switch {
	case postgresError.Code == syntaxError && strings.Contains(postgresError.Message, "multiple commands"):
		return errors.New("query runs exactly one statement; send each one in its own call")
	case postgresError.Code == syntaxError:
		return fmt.Errorf("query runs one SELECT, VALUES, TABLE or WITH statement: %w", err)
	case postgresError.Code == readOnlySQLTransaction:
		return fmt.Errorf("the admin server is read-only: %w", err)
	case postgresError.Code == queryCanceled:
		return fmt.Errorf("the statement was canceled, probably by the %s statement timeout: %w", adminmcp.StatementTimeout, err)
	case postgresError.Code == insufficientPrivilege && strings.Contains(postgresError.Message, "lobby_sessions"):
		return fmt.Errorf("the database role may read only some columns of lobby_sessions, so name the columns: %w", err)
	case postgresError.Code == insufficientPrivilege:
		return fmt.Errorf("the database role is not permitted to do this: %w", err)
	}
	return err
}
