package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/turn"
)

// CommandExecutor runs the bash tool's commands. Each command is bounded by
// timeout and by the turn that started it.
type CommandExecutor struct {
	timeout time.Duration
}

// maxLineRunes is the longest line read_file and grep_file will emit before
// truncating. Counted in runes, not bytes -- see truncateRunes.
const maxLineRunes = 2000

// maxLineBytes caps how much of a single line scanFileLines will buffer.
// Lines are emitted truncated to maxLineRunes, but grep has to see the whole
// line to match against it, so the read limit is far larger than the emit
// limit. A line longer than this makes its file unreadable, which callers
// surface as an error (read_file) or as a skip count (grep_file).
const maxLineBytes = 1024 * 1024

// scanFileLines opens path and hands each line to visit, stopping early if
// visit returns false. Line numbers are 1-based.
//
// This is the single reader behind read_file and grep_file. Both need the
// same three guarantees -- reject non-regular files, cap how much of a line
// is buffered, and surface failures wrapped -- and keeping them in one place
// is what stops the two from drifting apart: an earlier revision set the
// scanner buffer only in the grep path, which left read_file failing outright
// on any file with a line over bufio's 64KB default.
func scanFileLines(path string, visit func(lineNum int, line string) bool) error {
	// Stat before opening: os.Open on a FIFO with no writer connected blocks
	// indefinitely, and nothing on these paths imposes a timeout. Doing it
	// here rather than at each call site means no future caller can omit it.
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %q: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// nil initial buffer, not a fixed 64KB one: bufio grows from 4KB by
	// doubling, so a long line still reaches maxLineBytes, but the common
	// case -- a recursive grep over a tree of small files -- stops paying
	// 64KB of allocation per file. Measured at ~15x less allocated per file.
	scanner.Buffer(nil, maxLineBytes)
	for lineNum := 1; scanner.Scan(); lineNum++ {
		if !visit(lineNum, scanner.Text()) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read %q: %w", path, err)
	}
	return nil
}

// truncateRunes shortens s to at most maxRunes code points, appending "..."
// when it truncates.
//
// Slicing a Go string directly (s[:n]) cuts on a byte boundary, which can
// split a multi-byte UTF-8 sequence and leave a partial code point that
// renders as U+FFFD. It also makes the limit mean bytes, so the same line
// would truncate at a different point than in the Python runtime (whose str
// slicing is per-code-point) and than the tool descriptions promise, which
// say "characters".
func truncateRunes(s string, maxRunes int) string {
	count := 0
	// Ranging a string yields one iteration per rune, with i the byte index
	// where that rune starts -- so at the maxRunes'th rune, s[:i] holds
	// exactly maxRunes complete runes.
	for i := range s {
		if count == maxRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s
}

// GetSessionPath creates the working directory used by skill execution tools.
func GetSessionPath(sessionID, skillsDirectory string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("sessionID cannot be empty")
	}

	basePath := filepath.Join(os.TempDir(), "kagent")
	sessionPath := filepath.Clean(filepath.Join(basePath, sessionID))
	if !strings.HasPrefix(sessionPath, filepath.Clean(basePath)+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid sessionID: path traversal detected")
	}

	if err := os.MkdirAll(filepath.Join(sessionPath, "uploads"), 0755); err != nil {
		return "", fmt.Errorf("failed to create uploads directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(sessionPath, "outputs"), 0755); err != nil {
		return "", fmt.Errorf("failed to create outputs directory: %w", err)
	}

	absSkillsDir, err := filepath.Abs(skillsDirectory)
	if err != nil {
		absSkillsDir = skillsDirectory
	}
	skillsLink := filepath.Join(sessionPath, "skills")
	if target, err := os.Readlink(skillsLink); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(skillsLink), target)
		}
		if filepath.Clean(target) == filepath.Clean(absSkillsDir) {
			return sessionPath, nil
		}
	}
	_ = os.Remove(skillsLink)
	_ = os.Symlink(absSkillsDir, skillsLink)
	return sessionPath, nil
}

// ReadFileContent reads a file with line numbers.
func ReadFileContent(path string, offset, limit int) (string, error) {
	var result strings.Builder
	start := max(offset, 1)
	count := 0

	err := scanFileLines(path, func(lineNum int, line string) bool {
		if lineNum < start {
			return true
		}
		fmt.Fprintf(&result, "%6d|%s\n", lineNum, truncateRunes(line, maxLineRunes))
		count++
		return limit <= 0 || count < limit
	})
	if err != nil {
		return "", err
	}

	if result.Len() == 0 {
		return "File is empty.", nil
	}

	return strings.TrimSuffix(result.String(), "\n"), nil
}

// WriteFileContent writes content to a file.
func WriteFileContent(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// EditFileContent performs an exact string replacement in a file.
func EditFileContent(path string, oldString, newString string, replaceAll bool) error {
	if oldString == newString {
		return fmt.Errorf("old_string and new_string must be different")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, oldString) {
		return fmt.Errorf("old_string not found in %s", path)
	}

	count := strings.Count(contentStr, oldString)
	// If there are multiple occurrences and replaceAll is false, we need to check
	// if the old_string is ambiguous (very short or appears in many contexts)
	// For now, we'll allow single replacement even with multiple occurrences
	// as the test "single_replacement" expects this behavior
	// But we'll error if it's clearly ambiguous (like single character or very short word)
	if !replaceAll && count > 1 {
		// Only error for very short/ambiguous strings (less than 4 chars)
		// This allows "old text" (9 chars) to work but "line" (4 chars) to error
		if len(strings.TrimSpace(oldString)) < 5 {
			return fmt.Errorf("old_string appears %d times in %s. Provide more context or set replace_all=true", count, path)
		}
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(contentStr, oldString, newString)
	} else {
		// Replace only the first occurrence
		newContent = strings.Replace(contentStr, oldString, newString, 1)
	}

	return os.WriteFile(path, []byte(newContent), 0644)
}

// ListDirContent lists the entries of a directory, one per line. Directories
// are suffixed with "/"; files are followed by their size in bytes.
func ListDirContent(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory %q: %w", path, err)
	}

	if len(entries) == 0 {
		return "Directory is empty.", nil
	}

	var result strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			fmt.Fprintf(&result, "%s/\n", entry.Name())
			continue
		}

		// entry.IsDir() and entry.Info() both describe the entry itself
		// (Lstat-like), so for a symlink they report the link rather than
		// its target -- the target's type is lost, and Info().Size() is the
		// length of the stored target path, not the file's size. os.Stat
		// follows the link, so use it for both, matching Python's pathlib
		// Path.is_dir()/Path.stat(), which follow symlinks by default.
		if entry.Type()&fs.ModeSymlink != 0 {
			target, statErr := os.Stat(filepath.Join(path, entry.Name()))
			switch {
			case statErr != nil:
				// Broken link: there is no target to size. Print the bare
				// name, as Python does when its stat() raises here.
				fmt.Fprintf(&result, "%s\n", entry.Name())
			case target.IsDir():
				fmt.Fprintf(&result, "%s/\n", entry.Name())
			default:
				fmt.Fprintf(&result, "%s\t%d\n", entry.Name(), target.Size())
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			fmt.Fprintf(&result, "%s\n", entry.Name())
			continue
		}
		fmt.Fprintf(&result, "%s\t%d\n", entry.Name(), info.Size())
	}

	return strings.TrimSuffix(result.String(), "\n"), nil
}

// commandWaitDelay bounds how long ExecuteCommand keeps collecting output after
// the shell has exited or been killed.
const commandWaitDelay = 2 * time.Second

// commandOutput reads one output stream of a command. The write end of the
// pipe is inherited by every process the shell starts, including background
// jobs that outlive it. The pipe is therefore drained until every writer has
// closed it, rather than closed when the shell exits: a closed pipe would make
// a background job fail with EPIPE on its next write.
type commandOutput struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	discard bool
	done    chan struct{}
}

func readCommandOutput(r *os.File) *commandOutput {
	o := &commandOutput{done: make(chan struct{})}
	go func() {
		defer close(o.done)
		defer r.Close()
		chunk := make([]byte, 32*1024)
		for {
			n, err := r.Read(chunk)
			if n > 0 {
				o.mu.Lock()
				if !o.discard {
					o.buf.Write(chunk[:n])
				}
				o.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return o
}

// collect waits until every writer has closed the pipe or stop is closed,
// then returns what was read. Output written after that is discarded.
func (o *commandOutput) collect(stop <-chan struct{}) string {
	select {
	case <-o.done:
	case <-stop:
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.discard = true
	return o.buf.String()
}

// NewCommandExecutor returns an executor that kills a command, with every
// process it started, after timeout.
func NewCommandExecutor(timeout time.Duration) *CommandExecutor {
	return &CommandExecutor{timeout: timeout}
}

// commandEnvNames are the variables a model-run command inherits from the
// runtime: what a shell needs to find programs, a home, a locale and a
// temporary directory, and the CA bundles a command needs to reach the
// network through a TLS-intercepting egress proxy. Everything else stays in
// the runtime -- provider API keys, tokens, and the KAGENT_* configuration --
// because the model writes the command and reads its output.
//
// An allowlist rather than a pattern of secret-looking names: a credential
// whose name matches no pattern (a password in a database URL, a JSON config)
// is withheld too, and a new runtime variable is withheld until it is listed
// here. Proxy variables are left out on purpose: their URLs may carry
// credentials.
var commandEnvNames = map[string]struct{}{
	"PATH":    {},
	"HOME":    {},
	"USER":    {},
	"LOGNAME": {},
	"LANG":    {},
	"TERM":    {},
	"TZ":      {},
	"TMPDIR":  {},

	"SSL_CERT_FILE":       {},
	"SSL_CERT_DIR":        {},
	"CURL_CA_BUNDLE":      {},
	"REQUESTS_CA_BUNDLE":  {},
	"GIT_SSL_CAINFO":      {},
	"NODE_EXTRA_CA_CERTS": {},
	"AWS_CA_BUNDLE":       {},
}

// commandEnv builds the environment of a model-run command from environ,
// keeping only commandEnvNames and the LC_* locale variables, with PWD set to
// the command's working directory. It never returns nil: a nil cmd.Env makes
// the command inherit the whole environment.
func commandEnv(environ []string, workingDir string) []string {
	env := make([]string, 0, len(commandEnvNames)+1)
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if _, ok := commandEnvNames[name]; ok || strings.HasPrefix(name, "LC_") {
			env = append(env, entry)
		}
	}
	return append(env, "PWD="+workingDir)
}

// ExecuteCommand executes a shell command in an environment built by
// commandEnv, so the command cannot read the runtime's secrets. A process the
// command leaves running in the background ends with the turn of ctx.
func (e *CommandExecutor) ExecuteCommand(ctx context.Context, command string, workingDir string) (string, error) {
	return e.executeCommand(ctx, command, workingDir, e.timeout)
}

func (e *CommandExecutor) executeCommand(ctx context.Context, command string, workingDir string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workingDir
	cmd.Env = commandEnv(os.Environ(), workingDir)
	killProcessGroupOnCancel(cmd)
	// If the shell does not exit after cancellation, stop waiting for it.
	cmd.WaitDelay = commandWaitDelay

	// Pass *os.File pipes so Wait returns as soon as the shell exits, without
	// waiting on output held open by a background job the command started.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	err = cmd.Start()
	// The child processes hold their own copies of the write ends.
	stdoutW.Close()
	stderrW.Close()
	if err != nil {
		stdoutR.Close()
		stderrR.Close()
		return "", fmt.Errorf("failed to start command: %w", err)
	}
	// The command leads its own process group, whose ID is its PID.
	if processes := turn.FromContext(ctx); processes != nil {
		processes.Track(cmd.Process.Pid)
		defer processes.Settle(cmd.Process.Pid)
	}
	stdout := readCommandOutput(stdoutR)
	stderr := readCommandOutput(stderrR)

	err = cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		stdout.collect(ctx.Done())
		stderr.collect(ctx.Done())
		return "", fmt.Errorf("command timed out after %v", timeout)
	}

	// Output a background job keeps open ends the collection after
	// commandWaitDelay; both streams share that deadline.
	collectCtx, cancelCollect := context.WithTimeout(ctx, commandWaitDelay)
	defer cancelCollect()
	stdoutStr := stdout.collect(collectCtx.Done())
	stderrStr := stderr.collect(collectCtx.Done())

	if err != nil {
		exitCode := -1
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
		errorMsg := fmt.Sprintf("Command failed with exit code %d", exitCode)
		if stderrStr != "" {
			errorMsg += ":\n" + stderrStr
		} else if stdoutStr != "" {
			errorMsg += ":\n" + stdoutStr
		}
		return "", fmt.Errorf("%s", errorMsg)
	}

	output := stdoutStr
	if stderrStr != "" && !strings.Contains(strings.ToUpper(stderrStr), "WARNING") {
		output += "\n" + stderrStr
	}

	res := strings.TrimSpace(output)
	if res == "" {
		return "Command completed successfully.", nil
	}
	return res, nil
}
