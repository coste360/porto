// Command porto is the single binary of Porto: the server and the operator tools.
//
//	porto serve
//	porto migrate up | status [--check]
//	porto admin create --email E --name N --org ORG [--marina NAME --slug SLUG --timezone TZ --locale it] [--password-stdin]
//	porto seed demo [--password-stdin]
//	porto backup --out FILE        ("-" = stdout)
//	porto restore --in FILE [--force]
//	porto version
//
// Exit codes: 0 ok, 1 runtime error, 2 usage error. Messages go to stderr.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

// usageError makes the command exit with status 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

const usage = `usage:
  porto serve
  porto migrate up | status [--check]
  porto admin create --email E --name N --org ORG [--marina NAME --slug SLUG --timezone TZ --locale LOCALE] [--password-stdin]
  porto seed demo [--password-stdin]
  porto backup --out FILE
  porto restore --in FILE [--force]
  porto version
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], env{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, lookup: os.LookupEnv})
	stop()
	os.Exit(code)
}

// env is the process environment of a command (replaced in tests).
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	lookup         func(string) (string, bool)
}

func run(ctx context.Context, args []string, e env) int {
	err := dispatch(ctx, args, e)
	if err == nil {
		return 0
	}
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(e.stderr, "porto: %s\n\n%s", ue.msg, usage)
		return 2
	}
	var ce checkFailed
	if errors.As(err, &ce) {
		fmt.Fprintf(e.stderr, "porto: %s\n", ce.Error())
		return 1
	}
	fmt.Fprintf(e.stderr, "porto: %v\n", err)
	return 1
}

func dispatch(ctx context.Context, args []string, e env) error {
	if len(args) == 0 {
		return usagef("missing command")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cmdServe(ctx, rest, e)
	case "migrate":
		return cmdMigrate(ctx, rest, e)
	case "admin":
		if len(rest) == 0 || rest[0] != "create" {
			return usagef("unknown admin command (want: admin create)")
		}
		return cmdAdminCreate(ctx, rest[1:], e)
	case "seed":
		if len(rest) == 0 || rest[0] != "demo" {
			return usagef("unknown seed command (want: seed demo)")
		}
		return cmdSeedDemo(ctx, rest[1:], e)
	case "backup":
		return cmdBackup(ctx, rest, e)
	case "restore":
		return cmdRestore(ctx, rest, e)
	case "version", "--version", "-v":
		return cmdVersion(e)
	case "help", "--help", "-h":
		fmt.Fprint(e.stdout, usage)
		return nil
	default:
		return usagef("unknown command %q", cmd)
	}
}
