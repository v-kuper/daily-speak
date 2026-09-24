package migrations

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

//go:embed 0001_init.sql
var InitialSchema string

//go:embed *.sql
var files embed.FS

var migrationName = regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+\.sql$`)

type Migration struct {
	Name     string
	SQL      string
	Checksum string
}

func All() ([]Migration, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	migrations := make([]Migration, 0, len(entries))
	versions := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		match := migrationName.FindStringSubmatch(name)
		if entry.IsDir() || match == nil {
			return nil, fmt.Errorf("invalid migration name %q", name)
		}
		if versions[match[1]] {
			return nil, fmt.Errorf("duplicate migration version %s", match[1])
		}
		versions[match[1]] = true
		data, err := files.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return nil, fmt.Errorf("migration %s is empty", name)
		}
		if err := rejectTransactionControl(string(data)); err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		checksum := sha256.Sum256(data)
		migrations = append(migrations, Migration{
			Name: name, SQL: string(data), Checksum: hex.EncodeToString(checksum[:]),
		})
	}
	// fs.ReadDir returns entries in filename order. The fixed-width version is
	// the prefix, so this is also the migration execution order.
	return migrations, nil
}

// Migration files run inside a transaction owned by the API. Transaction
// control in a file could release the advisory lock before the ledger insert.
// The scanner only examines statement-leading SQL outside quotes and comments.
func rejectTransactionControl(sql string) error {
	atStart := true
	for i := 0; i < len(sql); {
		switch {
		case isSQLSpace(sql[i]):
			i++
		case i+1 < len(sql) && sql[i:i+2] == "--":
			i += 2
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case i+1 < len(sql) && sql[i:i+2] == "/*":
			i += 2
			depth := 1
			for i < len(sql) && depth > 0 {
				switch {
				case i+1 < len(sql) && sql[i:i+2] == "/*":
					depth++
					i += 2
				case i+1 < len(sql) && sql[i:i+2] == "*/":
					depth--
					i += 2
				default:
					i++
				}
			}
			if depth != 0 {
				return fmt.Errorf("unterminated block comment")
			}
		case sql[i] == '\'' || sql[i] == '"':
			quote := sql[i]
			escapeLiteral := quote == '\'' && i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e') && (i == 1 || !isSQLWord(sql[i-2]))
			i++
			closed := false
			for i < len(sql) {
				if escapeLiteral && sql[i] == '\\' && i+1 < len(sql) {
					i += 2
					continue
				}
				if sql[i] == quote {
					if i+1 < len(sql) && sql[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return fmt.Errorf("unterminated SQL quote")
			}
			atStart = false
		case sql[i] == '$':
			end := i + 1
			for end < len(sql) && isSQLWord(sql[end]) {
				end++
			}
			if end < len(sql) && sql[end] == '$' {
				delimiter := sql[i : end+1]
				closeAt := strings.Index(sql[end+1:], delimiter)
				if closeAt < 0 {
					return fmt.Errorf("unterminated dollar quote")
				}
				i = end + 1 + closeAt + len(delimiter)
			} else {
				i++
			}
			atStart = false
		case sql[i] == ';':
			atStart = true
			i++
		case isSQLWord(sql[i]):
			end := i + 1
			for end < len(sql) && isSQLWord(sql[end]) {
				end++
			}
			if atStart {
				switch strings.ToUpper(sql[i:end]) {
				case "BEGIN", "COMMIT", "ROLLBACK", "START", "END", "ABORT", "PREPARE":
					return fmt.Errorf("transaction control is not allowed inside a migration")
				}
			}
			atStart = false
			i = end
		default:
			atStart = false
			i++
		}
	}
	return nil
}

func isSQLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isSQLWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
