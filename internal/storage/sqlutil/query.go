package sqlutil

import (
	"fmt"
	"strings"
)

// QueryBuilder assembles SELECT queries with composable clauses.
type QueryBuilder struct {
	table      string
	columns    []string
	conditions []string
	args       []any
	orderBys   []string
	limit      *int
	offset     *int
}

// NewQueryBuilder creates a builder targeting table.
func NewQueryBuilder(table string) *QueryBuilder {
	return &QueryBuilder{
		table:   table,
		columns: []string{"*"},
	}
}

// Select sets the columns to select. When no columns are provided "*" is used.
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder {
	if len(columns) == 0 {
		qb.columns = []string{"*"}
		return qb
	}
	qb.columns = append([]string{}, columns...)
	return qb
}

// Where appends an arbitrary WHERE predicate and its arguments.
func (qb *QueryBuilder) Where(condition string, args ...any) *QueryBuilder {
	if strings.TrimSpace(condition) == "" {
		return qb
	}
	qb.conditions = append(qb.conditions, condition)
	qb.args = append(qb.args, args...)
	return qb
}

// WhereEq adds a "column = ?" predicate.
func (qb *QueryBuilder) WhereEq(column string, value any) *QueryBuilder {
	return qb.Where(fmt.Sprintf("%s = ?", column), value)
}

// WhereIn adds a "column IN (?, ?, ...)" predicate.
func (qb *QueryBuilder) WhereIn(column string, values []any) *QueryBuilder {
	if len(values) == 0 {
		return qb
	}
	placeholders := make([]string, len(values))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	predicate := fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", "))
	qb.conditions = append(qb.conditions, predicate)
	qb.args = append(qb.args, values...)
	return qb
}

// OrderBy appends an ORDER BY clause. Direction should typically be ASC or DESC.
func (qb *QueryBuilder) OrderBy(column, direction string) *QueryBuilder {
	column = strings.TrimSpace(column)
	if column == "" {
		return qb
	}
	dir := strings.TrimSpace(direction)
	if dir != "" {
		qb.orderBys = append(qb.orderBys, fmt.Sprintf("%s %s", column, dir))
	} else {
		qb.orderBys = append(qb.orderBys, column)
	}
	return qb
}

// Limit constrains the number of returned rows.
func (qb *QueryBuilder) Limit(limit int) *QueryBuilder {
	if limit <= 0 {
		qb.limit = nil
		return qb
	}
	qb.limit = &limit
	return qb
}

// Offset skips a number of rows before returning results.
func (qb *QueryBuilder) Offset(offset int) *QueryBuilder {
	if offset < 0 {
		qb.offset = nil
		return qb
	}
	qb.offset = &offset
	return qb
}

// Build renders the SQL query string and ordered arguments.
func (qb *QueryBuilder) Build() (string, []any) {
	if qb.table == "" {
		return "", nil
	}
	parts := []string{fmt.Sprintf("SELECT %s", strings.Join(qb.columns, ", "))}
	parts = append(parts, fmt.Sprintf("FROM %s", qb.table))
	if len(qb.conditions) > 0 {
		parts = append(parts, fmt.Sprintf("WHERE %s", strings.Join(qb.conditions, " AND ")))
	}
	if len(qb.orderBys) > 0 {
		parts = append(parts, fmt.Sprintf("ORDER BY %s", strings.Join(qb.orderBys, ", ")))
	}
	if qb.limit != nil {
		parts = append(parts, fmt.Sprintf("LIMIT %d", *qb.limit))
	}
	if qb.offset != nil {
		parts = append(parts, fmt.Sprintf("OFFSET %d", *qb.offset))
	}
	return strings.Join(parts, " "), append([]any{}, qb.args...)
}
