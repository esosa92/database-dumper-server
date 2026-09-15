package dumper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"database-dumper-server/db"
)

type Logger interface {
	Printf(format string, args ...any)
	Writer() io.Writer
}

type Options struct {
	Tables         []string
	OnlyCoreConfig bool
	Part           string
}

type Result struct {
	Files []string
}

func sshCommand(ctx context.Context, s *db.Server, script string) *exec.Cmd {
	var cmd *exec.Cmd
	if s.SSHPass == "" {
		cmd = exec.CommandContext(ctx, "ssh", s.SSHHost, "/bin/bash", "-s", s.RemoteEnvPath)
	} else {
		cmd = exec.CommandContext(ctx, "sshpass", "-e", "ssh", s.SSHHost, "/bin/bash", "-s", s.RemoteEnvPath)
		cmd.Env = append(os.Environ(), "SSHPASS="+s.SSHPass)
	}
	cmd.Stdin = strings.NewReader(script)
	return cmd
}

func commandString(cmd *exec.Cmd) string {
	return cmd.Path + " " + strings.Join(cmd.Args[1:], " ")
}

func executeDumpScript(ctx context.Context, s *db.Server, script string, log Logger) (remoteFile, error) {
	cmd := sshCommand(ctx, s, script)
	log.Printf("%s", script)
	log.Printf("Command to be executed: %s", commandString(cmd))

	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(log.Writer(), &out)
	cmd.Stderr = log.Writer()

	if err := cmd.Run(); err != nil {
		return remoteFile{}, fmt.Errorf("SSH command finished with error: %v", err)
	}

	var remote remoteFile
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Generated filename:"); ok {
			remote.Path = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "Generated size:"); ok {
			remote.Size, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	if remote.Path == "" {
		return remote, fmt.Errorf("failed to capture the generated filename")
	}
	return remote, nil
}

type remoteFile struct {
	Path string
	Size int64
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func watchDownload(ctx context.Context, localPath string, total int64, log Logger) func() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(localPath)
				if err != nil {
					continue
				}
				if total > 0 {
					log.Printf("Download progress: %s / %s (%.0f%%)", humanBytes(info.Size()), humanBytes(total), float64(info.Size())*100/float64(total))
				} else {
					log.Printf("Download progress: %s", humanBytes(info.Size()))
				}
			}
		}
	}()
	return func() { close(stop) }
}

func getRemoteTableList(ctx context.Context, s *db.Server, log Logger) ([]string, error) {
	cmd := sshCommand(ctx, s, getTablesScript)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = log.Writer()

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("SSH command to list tables failed: %v", err)
	}

	var tables []string
	for _, line := range strings.Split(out.String(), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			tables = append(tables, t)
		}
	}
	return tables, nil
}

func scpFile(ctx context.Context, s *db.Server, remote remoteFile, localName string, log Logger) (string, error) {
	log.Printf("Starting SCP transfer...")
	if strings.TrimSpace(localName) == "" {
		localName = path.Base(remote.Path)
	}
	localPath := filepath.Join(s.LocalPath, localName)

	var cmd *exec.Cmd
	if s.SSHPass != "" {
		cmd = exec.CommandContext(ctx, "sshpass", "-e", "scp", s.SSHHost+":"+remote.Path, localPath)
		cmd.Env = append(os.Environ(), "SSHPASS="+s.SSHPass)
	} else {
		cmd = exec.CommandContext(ctx, "scp", s.SSHHost+":"+remote.Path, localPath)
	}
	log.Printf("Command to be executed: %s", commandString(cmd))

	if err := os.MkdirAll(s.LocalPath, 0o755); err != nil {
		return "", fmt.Errorf("could not create local_path '%s': %v", s.LocalPath, err)
	}

	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	stopWatch := watchDownload(ctx, localPath, remote.Size, log)
	err := cmd.Run()
	stopWatch()
	if err != nil {
		return "", fmt.Errorf("SCP command finished with error: %v", err)
	}
	log.Printf("SCP transfer completed successfully")
	return localPath, nil
}

func isTransientSCPError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"broken pipe", "connection closed", "connection reset", "timeout", "exit status 255"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func scpFileWithRetry(ctx context.Context, s *db.Server, remote remoteFile, localName string, log Logger) (string, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		local, err := scpFile(ctx, s, remote, localName, log)
		if err == nil {
			return local, nil
		}
		lastErr = err
		if ctx.Err() != nil || !isTransientSCPError(err) || attempt == maxAttempts {
			break
		}
		wait := time.Duration(attempt*2) * time.Second
		log.Printf("Transient SCP error detected (attempt %d/%d): %v", attempt, maxAttempts, err)
		log.Printf("Retrying SCP in %s...", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

func sanitizeToken(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "#"))
	if v == "" {
		return "part"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, v)
}

func safeID(s *db.Server) string {
	id := strings.TrimSpace(s.ID)
	if id == "" {
		id = "default"
	}
	return strings.NewReplacer("/", "_", " ", "_").Replace(id)
}

func progressFilePath(s *db.Server) string {
	return filepath.Join(s.LocalPath, safeID(s)+"_progress.txt")
}

func loadProgressFile(p string) (map[string]bool, error) {
	done := map[string]bool{}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return done, nil
	}
	if err != nil {
		return done, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			done[t] = true
		}
	}
	return done, nil
}

func appendProgressFile(p, table string) error {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, table)
	return err
}

func filterPartitionsByTag(parts []Partition, filter string) ([]Partition, error) {
	norm := func(v string) string {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "#"))
	}
	filter = norm(filter)
	if filter == "" {
		return parts, nil
	}
	var tags []string
	for _, p := range parts {
		if strings.EqualFold(norm(p.Tag), filter) {
			return []Partition{p}, nil
		}
		if t := norm(p.Tag); t != "" {
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("partition '%s' not found (no tagged partitions found)", filter)
	}
	return nil, fmt.Errorf("partition '%s' not found. Available partitions: %s", filter, strings.Join(tags, ", "))
}

func Run(ctx context.Context, server db.Server, opts Options, log Logger) (Result, error) {
	s := &server
	var res Result

	if s.RemoteEnvPath == "" {
		return res, fmt.Errorf("no remote_env_path configured")
	}
	if s.SSHHost == "" {
		return res, fmt.Errorf("no ssh_host configured")
	}
	if s.LocalPath == "" {
		s.LocalPath = "./"
	}

	log.Printf("Will connect to: %s", s.SSHHost)
	log.Printf("Magento env.php abs location in server is: %s", s.RemoteEnvPath)

	if opts.OnlyCoreConfig {
		s.OnlyCoreConfig = true
		s.OnlyTables = ""
		s.SingleTableMode = false
	}

	dumpAndFetch := func(tables []string, localName string) error {
		return dumpAndFetchNamed(ctx, s, tables, localName, log, &res)
	}

	if len(opts.Tables) > 0 {
		s.SingleTableMode = false
		s.OnlyCoreConfig = false
		s.OnlyTables = ""
		s.IgnoreTables = nil
		s.WithCoreConfig = true
		log.Printf("Ad-hoc table mode: %d tables: %s", len(opts.Tables), strings.Join(opts.Tables, ", "))
		return res, dumpAndFetch(opts.Tables, "")
	}

	if s.SingleTableMode {
		progress := progressFilePath(s)
		done, err := loadProgressFile(progress)
		if err != nil {
			log.Printf("Warning: could not load progress file '%s': %v", progress, err)
		}
		if len(done) > 0 {
			log.Printf("Resuming: %d tables already downloaded (loaded from %s)", len(done), progress)
			existing := map[string]bool{}
			for _, t := range s.IgnoreTables {
				existing[t] = true
			}
			for t := range done {
				if !existing[t] {
					s.IgnoreTables = append(s.IgnoreTables, t)
				}
			}
		}

		remote, err := getRemoteTableList(ctx, s, log)
		if err != nil {
			return res, err
		}
		tables, err := selectTablesForSingleTableMode(s, remote, log)
		if err != nil {
			return res, err
		}

		log.Printf("Single table mode: %d tables to download", len(tables))
		for _, table := range tables {
			log.Printf("Dumping table: %s", table)
			if err := dumpAndFetch([]string{table}, ""); err != nil {
				return res, fmt.Errorf("table %s: %w", table, err)
			}
			if err := appendProgressFile(progress, table); err != nil {
				log.Printf("Warning: could not update progress file for table '%s': %v", table, err)
			}
		}
		log.Printf("All tables downloaded. Progress file: %s", progress)
		return res, nil
	}

	if strings.TrimSpace(s.OnlyTables) != "" && !s.OnlyCoreConfig {
		parts := ParsePartitions(s.OnlyTables)
		if len(parts) == 0 {
			return res, fmt.Errorf("no tables found in only_tables")
		}
		parts, err := filterPartitionsByTag(parts, opts.Part)
		if err != nil {
			return res, err
		}
		if len(parts) > 1 {
			log.Printf("only_tables partition mode: %d dumps will be generated", len(parts))
		}
		for i, part := range parts {
			label := part.Tag
			if label == "" {
				label = fmt.Sprintf("%d", i+1)
			}
			localName := ""
			if len(parts) > 1 || part.Tag != "" {
				log.Printf("Generating dump part '%s' (%d/%d) with %d tables", label, i+1, len(parts), len(part.Tables))
				localName = sanitizeToken(label) + ".__base__"
			}
			if err := dumpAndFetch(part.Tables, localName); err != nil {
				return res, fmt.Errorf("dump part '%s': %w", label, err)
			}
		}
		return res, nil
	}

	return res, dumpAndFetch(nil, "")
}

func dumpAndFetchNamed(ctx context.Context, s *db.Server, tables []string, localName string, log Logger, res *Result) error {
	script, err := buildDumpScript(s, tables, log)
	if err != nil {
		return err
	}
	remote, err := executeDumpScript(ctx, s, script, log)
	if err != nil {
		return err
	}
	log.Printf("File name: %s (%s)", remote.Path, humanBytes(remote.Size))
	if localName != "" {
		localName = strings.ReplaceAll(localName, "__base__", path.Base(remote.Path))
	}
	local, err := scpFileWithRetry(ctx, s, remote, localName, log)
	if err != nil {
		return err
	}
	res.Files = append(res.Files, local)
	return nil
}
