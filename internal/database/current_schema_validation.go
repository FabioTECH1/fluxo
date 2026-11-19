package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type SchemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

type schemaObject struct{ kind, name string }

// ValidateCurrentSchema checks canonical schema and integrity without changing the
// supplied database. It never runs historical conversions or uses a global handle.
func ValidateCurrentSchema(ctx context.Context, q SchemaQueryer) error {
	return ValidateCurrentSchemaMode(ctx, q, false)
}

// ValidateCurrentSchemaMode optionally collects independent schema failures.
// Metadata comparisons are independent of application rows. Inspection can
// report differences in several objects without querying untrusted row shapes.
func ValidateCurrentSchemaMode(ctx context.Context, q SchemaQueryer, collectAll bool) (resultErr error) {
	var failures []error
	defer func() {
		if len(failures) > 0 {
			resultErr = errors.Join(append(failures, resultErr)...)
		}
	}()
	ref, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer ref.Close()
	ref.SetMaxOpenConns(1)
	if _, err = ref.ExecContext(ctx, CurrentSchemaSQL()); err != nil {
		return fmt.Errorf("reference schema: %w", err)
	}
	rows, err := ref.QueryContext(ctx, "SELECT type,name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if err != nil {
		return err
	}
	var objects []schemaObject
	for rows.Next() {
		var x schemaObject
		if err = rows.Scan(&x.kind, &x.name); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range objects {
		if err = compareSchemaObject(ctx, q, ref, x); err != nil {
			if !collectAll {
				return err
			}
			failures = append(failures, err)
		}
	}
	actualTriggers, err := q.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='trigger'")
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, o := range objects {
		if o.kind == "trigger" {
			known[o.name] = true
		}
	}
	for actualTriggers.Next() {
		var name string
		if err = actualTriggers.Scan(&name); err != nil {
			actualTriggers.Close()
			return err
		}
		if !known[name] {
			err = fmt.Errorf("unexpected trigger %s requires review", name)
			if !collectAll {
				actualTriggers.Close()
				return err
			}
			failures = append(failures, err)
		}
	}
	err = actualTriggers.Err()
	actualTriggers.Close()
	if err != nil {
		return err
	}
	// Inline uniqueness constraints are not named indexes; compare their
	// ordered key columns separately, including expression and partial indexes.
	for _, x := range objects {
		if x.kind != "table" {
			continue
		}
		want, e := uniqueKeys(ctx, ref, x.name)
		if e != nil {
			return e
		}
		got, e := uniqueKeys(ctx, q, x.name)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(want, got) {
			err = fmt.Errorf("table %s uniqueness differs", x.name)
			if !collectAll {
				return err
			}
			failures = append(failures, err)
		}
	}
	var integrity string
	if err = q.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		err = fmt.Errorf("SQLite integrity check failed")
		if !collectAll {
			return err
		}
		failures = append(failures, err)
	}
	violations, err := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer violations.Close()
	if violations.Next() {
		return fmt.Errorf("database relationship violation")
	}
	return violations.Err()
}

func compareSchemaObject(ctx context.Context, q SchemaQueryer, ref *sql.DB, x schemaObject) error {
	var err error
	var actualType string
	if err = q.QueryRowContext(ctx, "SELECT type FROM sqlite_master WHERE name=?", x.name).Scan(&actualType); err != nil || actualType != x.kind {
		return fmt.Errorf("required %s %s is missing or invalid", x.kind, x.name)
	}
	if x.kind == "table" {
		var wantSQL, gotSQL string
		if err = ref.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", x.name).Scan(&wantSQL); err != nil {
			return err
		}
		if err = q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", x.name).Scan(&gotSQL); err != nil {
			return err
		}
		if !reflect.DeepEqual(tableSemantics(wantSQL), tableSemantics(gotSQL)) {
			return fmt.Errorf("table %s CHECK, collation, or allocation semantics differ", x.name)
		}
		want, err := pragmaRows(ctx, ref, "table_xinfo", x.name)
		if err != nil {
			return err
		}
		got, err := pragmaRows(ctx, q, "table_xinfo", x.name)
		if err != nil {
			return err
		}
		// Column order differs legitimately after historical ADD COLUMN.
		for _, row := range want {
			row[0] = ""
		}
		for _, row := range got {
			row[0] = ""
		}
		sort.Slice(want, func(i, j int) bool { return want[i][1] < want[j][1] })
		sort.Slice(got, func(i, j int) bool { return got[i][1] < got[j][1] })
		// Historical sites preserve their custom-script default. All current
		// creation paths explicitly select mode; existing row values stay intact.
		if x.name == "sites" {
			columns := make([][]string, 0, len(got))
			for _, row := range got {
				// 0.3.6 added this plain storage column; 0.3.7 removed
				// its consumers without dropping existing user commands.
				// Retain it as inert historical data, with its exact original
				// metadata. Generated or otherwise altered columns still fail.
				if reflect.DeepEqual(row, []string{"", "post_deploy_script", "TEXT", "0", "''", "0", "0"}) {
					continue
				}
				if row[1] == "deploy_script_mode" && row[4] == "'legacy'" {
					row[4] = "'managed'"
				}
				columns = append(columns, row)
			}
			got = columns
		}
		// Published backup tables originally defaulted to 'fluxo'. Current
		// destination creation supplies the prefix explicitly; preserve both
		// the historical DDL and existing object paths instead of rewriting
		// them to the newer fresh-install default.
		if x.name == "backup_destinations" {
			for _, row := range got {
				if row[1] == "prefix" && row[4] == "'fluxo'" {
					row[4] = "'fluxo-backups'"
				}
			}
		}
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("table %s differs from the supported bridge schema", x.name)
		}
		for _, pragma := range []string{"foreign_key_list"} {
			want, e := pragmaRows(ctx, ref, pragma, x.name)
			if e != nil {
				return e
			}
			got, e := pragmaRows(ctx, q, pragma, x.name)
			if e != nil {
				return e
			}
			if !reflect.DeepEqual(want, got) {
				return fmt.Errorf("table %s relationships differ", x.name)
			}
		}
	} else {
		var want, got string
		if err = ref.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", x.name).Scan(&want); err != nil {
			return err
		}
		if err = q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", x.name).Scan(&got); err != nil {
			return err
		}
		if normalizeSQL(want) != normalizeSQL(got) {
			return fmt.Errorf("%s %s definition differs", x.kind, x.name)
		}
	}
	return nil
}

func normalizeSQL(s string) string {
	tokens := sqlTokens(s)
	var out []string
	for i := 0; i < len(tokens); i++ {
		if i+2 < len(tokens) && tokens[i] == "if" && tokens[i+1] == "not" && tokens[i+2] == "exists" {
			i += 2
			continue
		}
		if tokens[i] != ";" {
			out = append(out, tokens[i])
		}
	}
	return strings.Join(out, " ")
}

// Keep SQL string literals case-sensitive while normalizing identifiers and
// whitespace. SQLite's parser remains authoritative for executable SQL.
func sqlTokens(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "--" {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return append(out, "invalid-comment")
			}
			i += end + 4
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			start := i
			i++
			for i < len(s) {
				if s[i] == end {
					if i+1 < len(s) && s[i+1] == end && c != '[' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			v := s[start:i]
			if c != '\'' && len(v) >= 2 {
				v = strings.ToLower(v[1 : len(v)-1])
			}
			out = append(out, v)
			continue
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			start := i
			for i < len(s) {
				b := s[i]
				if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_') {
					break
				}
				i++
			}
			out = append(out, strings.ToLower(s[start:i]))
			continue
		}
		out = append(out, string(c))
		i++
	}
	return out
}

func tableSemantics(s string) []string {
	tokens := sqlTokens(s)
	var result []string
	for i, t := range tokens {
		// No canonical table uses an explicit conflict policy. In particular,
		// REPLACE can delete an existing administrator on an ordinary INSERT.
		if t == "on" && i+2 < len(tokens) && tokens[i+1] == "conflict" {
			result = append(result, "conflict-policy:"+tokens[i+2])
		}
		if t == "autoincrement" || t == "strict" {
			result = append(result, t)
		}
		if t == "without" && i+1 < len(tokens) && tokens[i+1] == "rowid" {
			result = append(result, "without rowid")
		}
		if t == "check" && i+1 < len(tokens) && tokens[i+1] == "(" {
			depth := 0
			for j := i + 1; j < len(tokens); j++ {
				if tokens[j] == "(" {
					depth++
				}
				if tokens[j] == ")" {
					depth--
					if depth == 0 {
						result = append(result, strings.Join(tokens[i:j+1], " "))
						break
					}
				}
			}
		}
		if t == "collate" && i+1 < len(tokens) {
			result = append(result, "collate "+tokens[i+1])
		}
	}
	sort.Strings(result)
	return result
}

func pragmaRows(ctx context.Context, q SchemaQueryer, pragma, name string) ([][]string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA "+pragma+"("+quote(name)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := [][]string{}
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range dest {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range values {
			if v != nil {
				row[i] = fmt.Sprint(v)
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func uniqueKeys(ctx context.Context, q SchemaQueryer, table string) ([]string, error) {
	list, err := pragmaRows(ctx, q, "index_list", table)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, idx := range list {
		if idx[2] != "1" {
			continue
		}
		rows, err := pragmaRows(ctx, q, "index_xinfo", idx[1])
		if err != nil {
			return nil, err
		}
		var cols []string
		for _, r := range rows {
			if r[5] == "1" {
				cols = append(cols, r[2]+":"+r[3]+":"+r[4])
			}
		}
		result = append(result, idx[3]+":"+idx[4]+":"+strings.Join(cols, ","))
	}
	sort.Strings(result)
	return result, nil
}
