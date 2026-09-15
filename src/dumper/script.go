package dumper

import (
	"fmt"
	"strings"

	"database-dumper-server/db"
)

const getTablesScript = `
#!/bin/bash
ENV_PHP_PATH=$1
DN="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "dbname" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/['][,]//")"
DH="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "host" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/['][,]//")"
DU="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "username" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/['][,]//")"
DP="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "password" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/[']$//" | sed "s/['][,]//")"
mysql -h$DH -u$DU -p$DP -e "SHOW TABLES;" "$DN" 2>/dev/null | tail -n +2
`

const dumpScript = `
#!/bin/bash

ENV_PHP_PATH=$1

DN="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "dbname" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/['][,]//")"
DH="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "host" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/['][,]//")"
DU="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "username" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/['][,]//")"
DP="$(grep "[\']db[\']" -A 20 "$ENV_PHP_PATH" | grep "password" | head -n1 | sed "s/.*[=][>][ ]*[']//" | sed "s/[']$//" | sed "s/['][,]//")"

echo "Starting dump file generation"
filename="/tmp/db.$DN.__name__.$(date +"%d-%m-%y_%H.%M.%S").$((1 + $RANDOM % 100000)).sql.gz"
( set -o pipefail; __dump_client__ -h$DH -u$DU -p$DP $DN --single-transaction --order-by-primary __skip__add__locks__ __skip__add__drop__table__ __net__buffer__length__ __skip__disable__keys__ __skip__extended__insert__ __skip__lock__tables__ __ignore__tables__ __add__purge__id__off__option__ __only__core__config__ __only__tables__ | sed -e 's/DEFINER[ ]*=[ ]*[^*]*\*/\*/' | gzip >"$filename" ) &
pid=$!
while kill -0 $pid 2>/dev/null; do
    sleep 10
    kill -0 $pid 2>/dev/null && echo "Dump progress: $(du -h "$filename" 2>/dev/null | cut -f1)"
done
wait $pid
status=$?
if [ $status -ne 0 ]; then
    echo "Dump command failed with status $status"
    rm -f "$filename"
    exit $status
fi
echo "Finished dump file generation"
echo "Generated size: $(stat -c %s "$filename")"
echo "Generated filename: $filename"
`

func applyOption(script, placeholder string, enabled bool, value string) string {
	if enabled {
		return strings.ReplaceAll(script, placeholder, value)
	}
	return strings.ReplaceAll(script, placeholder+" ", "")
}

func applyCommonOptions(script string, s *db.Server) string {
	script = applyOption(script, "__add__purge__id__off__option__", s.EnableSetGTIDPurgedOff, "--set-gtid-purged=OFF")
	script = applyOption(script, "__skip__extended__insert__", s.SkipExtendedInsert, "--skip-extended-insert")
	script = applyOption(script, "__skip__disable__keys__", s.SkipDisableKeys, "--skip-disable-keys")
	script = applyOption(script, "__skip__add__locks__", s.SkipAddLocks, "--skip-add-locks")
	script = applyOption(script, "__skip__lock__tables__", s.SkipLockTables, "--skip-lock-tables")
	script = applyOption(script, "__skip__add__drop__table__", s.SkipAddDropTable, "--skip-add-drop-table")
	script = applyOption(script, "__net__buffer__length__", s.NetBufferLength != "", "--net-buffer-length="+s.NetBufferLength)
	return script
}

func resolveDumpClient(s *db.Server, log Logger) string {
	client := strings.TrimSpace(s.DumpClient)
	if client == "" {
		return "mysqldump"
	}
	if strings.ContainsAny(client, " \t\r\n\"'`$|&;<>\\") {
		log.Printf("Warning: invalid dump_client '%s' in '%s'; falling back to mysqldump", s.DumpClient, s.ID)
		return "mysqldump"
	}
	return client
}

func buildDumpScript(s *db.Server, inlineOnlyTables []string, log Logger) (string, error) {
	script := applyCommonOptions(dumpScript, s)
	script = strings.ReplaceAll(script, "__dump_client__", resolveDumpClient(s, log))

	if s.OnlyCoreConfig && len(inlineOnlyTables) == 0 {
		script = strings.ReplaceAll(script, "__only__core__config__", "core_config_data")
		script = strings.ReplaceAll(script, "__name__", "core_config_data")
		script = strings.ReplaceAll(script, "__ignore__tables__ ", "")
		script = strings.ReplaceAll(script, "__only__tables__ ", "")
		return script, nil
	}

	script = strings.ReplaceAll(script, "__only__core__config__ ", "")

	onlyTables := inlineOnlyTables
	if len(onlyTables) == 0 && strings.TrimSpace(s.OnlyTables) != "" {
		onlyTables = FlattenPartitions(ParsePartitions(s.OnlyTables))
	}

	if len(onlyTables) > 0 {
		filtered := newIgnoreFilter(effectiveIgnoreTables(s), log).apply(onlyTables, log, " (only_tables filter)")
		if len(filtered) == 0 {
			return "", fmt.Errorf("no tables left after applying ignore_tables/with_core_config filters to only_tables")
		}
		script = strings.ReplaceAll(script, "__only__tables__", strings.Join(filtered, " "))
		script = strings.ReplaceAll(script, "__ignore__tables__ ", "")
		if len(filtered) == 1 {
			script = strings.ReplaceAll(script, "__name__", filtered[0])
		} else {
			script = strings.ReplaceAll(script, ".__name__", "")
		}
		return script, nil
	}

	filter := newIgnoreFilter(effectiveIgnoreTables(s), log)
	if len(filter.patterns) > 0 {
		log.Printf("Warning: wildcard ignore_tables patterns are ignored in non-single mode: %s", strings.Join(filter.patterns, ", "))
	}

	var ignoreArgs []string
	for _, t := range filter.exactList() {
		ignoreArgs = append(ignoreArgs, "--ignore-table=$DN."+t)
	}
	if len(ignoreArgs) > 0 {
		script = strings.ReplaceAll(script, "__ignore__tables__", strings.Join(ignoreArgs, " "))
	} else {
		script = strings.ReplaceAll(script, "__ignore__tables__ ", "")
	}

	script = strings.ReplaceAll(script, "__only__tables__ ", "")
	script = strings.ReplaceAll(script, ".__name__", "")
	return script, nil
}
