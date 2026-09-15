package dumper

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"database-dumper-server/db"
)

type Partition struct {
	Tag    string
	Tables []string
}

func ParsePartitions(content string) []Partition {
	var partitions []Partition
	var current []string
	var tag string

	flush := func() {
		if len(current) > 0 {
			partitions = append(partitions, Partition{Tag: tag, Tables: current})
			current = nil
		}
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			tag = ""
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			flush()
			tag = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			continue
		}
		current = append(current, trimmed)
	}
	flush()
	return partitions
}

func FlattenPartitions(parts []Partition) []string {
	var tables []string
	for _, p := range parts {
		tables = append(tables, p.Tables...)
	}
	return tables
}

func effectiveIgnoreTables(s *db.Server) []string {
	tables := append([]string{}, s.IgnoreTables...)
	if !s.WithCoreConfig && !s.OnlyCoreConfig {
		for _, t := range tables {
			if t == "core_config_data" {
				return tables
			}
		}
		tables = append(tables, "core_config_data")
	}
	return tables
}

func isWildcard(t string) bool {
	return strings.ContainsAny(t, "*?[]")
}

type ignoreFilter struct {
	exact    map[string]bool
	patterns []string
}

func newIgnoreFilter(tables []string, log Logger) ignoreFilter {
	f := ignoreFilter{exact: map[string]bool{}}
	for _, t := range tables {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !isWildcard(t) {
			f.exact[t] = true
			continue
		}
		if _, err := path.Match(t, "dummy_table"); err != nil {
			log.Printf("Warning: invalid ignore_tables wildcard pattern '%s'; pattern will be ignored", t)
			continue
		}
		f.patterns = append(f.patterns, t)
	}
	return f
}

func (f ignoreFilter) exactList() []string {
	var out []string
	for t := range f.exact {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func (f ignoreFilter) matches(table string) (string, bool) {
	if f.exact[table] {
		return "", true
	}
	for _, p := range f.patterns {
		if ok, _ := path.Match(p, table); ok {
			return p, true
		}
	}
	return "", false
}

func (f ignoreFilter) apply(tables []string, log Logger, context string) []string {
	var out []string
	for _, t := range tables {
		if pattern, skip := f.matches(t); skip {
			if pattern != "" {
				log.Printf("Skipping ignored table by pattern %s%s: %s", pattern, context, t)
			} else {
				log.Printf("Skipping ignored table%s: %s", context, t)
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

func selectTablesForSingleTableMode(s *db.Server, remote []string, log Logger) ([]string, error) {
	remoteSet := map[string]bool{}
	for _, t := range remote {
		remoteSet[t] = true
	}

	if s.OnlyCoreConfig {
		if !remoteSet["core_config_data"] {
			return nil, fmt.Errorf("core_config_data was requested but does not exist in remote database")
		}
		return []string{"core_config_data"}, nil
	}

	if strings.TrimSpace(s.OnlyTables) != "" {
		var selected, missing []string
		for _, t := range FlattenPartitions(ParsePartitions(s.OnlyTables)) {
			if remoteSet[t] {
				selected = append(selected, t)
			} else {
				missing = append(missing, t)
			}
		}
		if len(missing) > 0 {
			log.Printf("Warning: these tables from only_tables were not found remotely and will be skipped: %s", strings.Join(missing, ", "))
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("no tables from only_tables were found in remote database")
		}
		return selected, nil
	}

	selected := newIgnoreFilter(effectiveIgnoreTables(s), log).apply(remote, log, "")
	if len(selected) == 0 {
		return nil, fmt.Errorf("no valid tables left to dump after applying ignore rules")
	}
	return selected, nil
}
